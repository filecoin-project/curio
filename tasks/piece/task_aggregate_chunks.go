package piece

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

type AggregateChunksTask struct {
	db     *harmonydb.DB
	remote *paths.Remote
	sc     *ffi.SealCalls
}

func NewAggregateChunksTask(db *harmonydb.DB, remote *paths.Remote, sc *ffi.SealCalls) *AggregateChunksTask {
	return &AggregateChunksTask{
		db:     db,
		remote: remote,
		sc:     sc,
	}
}

func (a *AggregateChunksTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var chunks []struct {
		ID    string `db:"id"`
		Chunk int    `db:"chunk"`
		Size  int64  `db:"chunk_size"`
		RefID int64  `db:"ref_id"`
	}

	err = a.db.Select(ctx, &chunks, `
										SELECT
										    id, 
											chunk,
											chunk_size,
											ref_id 
										FROM 
											market_mk20_deal_chunk 
										WHERE 
											finalize_task_id = $1 
										  AND complete = TRUE 
										  AND finalize = TRUE 
										ORDER BY chunk ASC`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting chunk details: %w", err)
	}

	if len(chunks) == 0 {
		return false, xerrors.Errorf("no chunks to aggregate for task %d", taskID)
	}

	idStr := chunks[0].ID

	id, err := ulid.Parse(idStr)
	if err != nil {
		return false, xerrors.Errorf("parsing deal ID: %w", err)
	}

	deal, err := mk20.DealFromDB(ctx, a.db, id)
	if err != nil {
		return false, xerrors.Errorf("getting deal details: %w", err)
	}

	pi, err := deal.PieceInfo()
	if err != nil {
		return false, xerrors.Errorf("getting piece info: %w", err)
	}

	rawSize := int64(pi.RawSize)
	pcid := pi.PieceCIDV1
	psize := pi.Size
	pcid2 := deal.Data.PieceCID

	var readers []io.Reader
	var refIds []int64

	for _, chunk := range chunks {
		// get pieceID
		var pieceID []struct {
			PieceID storiface.PieceNumber `db:"piece_id"`
		}
		err = a.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, chunk.RefID)
		if err != nil {
			return false, xerrors.Errorf("getting pieceID: %w", err)
		}

		if len(pieceID) != 1 {
			return false, xerrors.Errorf("expected 1 pieceID, got %d", len(pieceID))
		}

		pr, err := a.sc.PieceReader(ctx, pieceID[0].PieceID)
		if err != nil {
			return false, xerrors.Errorf("getting piece reader: %w", err)
		}

		reader := pr

		defer func() {
			_ = pr.Close()
		}()
		readers = append(readers, reader)
		refIds = append(refIds, chunk.RefID)
	}

	rd := io.MultiReader(readers...)

	var parkedPieceID, pieceRefID int64
	var pieceParked bool

	comm, err := a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
            VALUES ($1, $2, $3, TRUE, TRUE) RETURNING id, complete`,
			pcid.String(), psize, rawSize).Scan(&parkedPieceID, &pieceParked)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, "/PUT").Scan(&pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("saving aggregated chunk details to DB: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit the transaction")
	}

	failed := true
	var cleanupChunks bool

	// Clean up piece park tables in case of failure
	// TODO: Figure out if there is a race condition with cleanup task
	defer func() {
		if cleanupChunks {
			_, serr := a.db.Exec(ctx, `DELETE FROM market_mk20_deal_chunk WHERE id = $1`, id.String())
			if serr != nil {
				log.Errorf("failed to delete market_mk20_deal_chunk entry: %w", serr)
			}
			_, serr = a.db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = ANY($1)`, refIds)
			if serr != nil {
				log.Errorf("failed to delete parked_piece_refs entry: %w", serr)
			}
		}
		if failed {
			_, ferr := a.db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, pieceRefID)
			if err != nil {
				log.Errorf("failed to delete parked_piece_refs entry: %w", ferr)
			}
		}
	}()

	// Write piece if not already complete
	if !pieceParked {
		pi, err := a.sc.WriteUploadPiece(ctx, storiface.PieceNumber(parkedPieceID), rawSize, rd, storiface.PathStorage)
		if err != nil {
			return false, xerrors.Errorf("writing aggregated piece data to storage: %w", err)
		}

		if !pi.PieceCID.Equals(pcid) {
			cleanupChunks = true
			return false, xerrors.Errorf("commP mismatch calculated %s and supplied %s", pi.PieceCID.String(), pcid.String())
		}

		if pi.Size != psize {
			cleanupChunks = true
			return false, xerrors.Errorf("commP size mismatch calculated %d and supplied %d", pi.Size, psize)
		}
	}

	// Update DB status of piece, deal, PDP
	comm, err = a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Update PoRep pipeline
		if deal.Products.DDOV1 != nil {
			spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
			if err != nil {
				return false, fmt.Errorf("getting provider ID: %w", err)
			}

			var rev mk20.RetrievalV1
			if deal.Products.RetrievalV1 != nil {
				rev = *deal.Products.RetrievalV1
			}

			ddo := deal.Products.DDOV1
			dealdata := deal.Data
			dealID := deal.Identifier.String()

			var allocationID interface{}
			if ddo.AllocationId != nil {
				allocationID = *ddo.AllocationId
			} else {
				allocationID = nil
			}

			aggregation := 0
			if dealdata.Format.Aggregate != nil {
				aggregation = int(dealdata.Format.Aggregate.Type)
			}

			if !pieceParked {
				_, err = tx.Exec(`UPDATE parked_pieces SET 
                         complete = TRUE 
                     WHERE id = $1 
                       AND complete = false`, pieceRefID)
				if err != nil {
					return false, xerrors.Errorf("marking piece park as complete: %w", err)
				}
			}

			pieceIDUrl := url.URL{
				Scheme: "pieceref",
				Opaque: fmt.Sprintf("%d", pieceRefID),
			}

			n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
		           id, sp_id, contract, client, piece_cid_v2, piece_cid,
		           piece_size, raw_size, url, offline, indexing, announce,
		           allocation_id, duration, piece_aggregation, deal_aggregation, started, downloaded, after_commp)
		       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, TRUE, TRUE, TRUE)`,
				dealID, spid, ddo.ContractAddress, deal.Client.String(), pcid2.String(), pcid.String(),
				psize, rawSize, pieceIDUrl.String(), false, rev.Indexing, rev.AnnouncePayload,
				allocationID, ddo.Duration, aggregation, aggregation)
			if err != nil {
				return false, xerrors.Errorf("inserting mk20 pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
			}

			_, err = tx.Exec(`DELETE FROM market_mk20_pipeline_waiting WHERE id = $1`, id.String())
			if err != nil {
				return false, xerrors.Errorf("deleting deal from mk20 pipeline waiting: %w", err)
			}

			_, err = tx.Exec(`DELETE FROM market_mk20_deal_chunk WHERE id = $1`, id.String())
			if err != nil {
				return false, xerrors.Errorf("deleting deal chunks from mk20 deal: %w", err)
			}

			_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = ANY($1)`, refIds)
			if err != nil {
				return false, xerrors.Errorf("deleting parked piece refs: %w", err)
			}
		}

		// Update PDP pipeline
		if deal.Products.PDPV1 != nil {
			pdp := deal.Products.PDPV1
			n, err := tx.Exec(`INSERT INTO pdp_pipeline (
            id, client, piece_cid_v2, piece_cid, piece_size, raw_size, proof_set_id, 
            extra_data, piece_ref, downloaded, deal_aggregation, aggr_index) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, $10, 0)`,
				id, deal.Client.String(), deal.Data.PieceCID.String(), pi.PieceCIDV1.String(), pi.Size, pi.RawSize, *pdp.ProofSetID,
				pdp.ExtraData, pieceRefID, deal.Data.Format.Aggregate.Type)
			if err != nil {
				return false, xerrors.Errorf("inserting in PDP pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("inserting in PDP pipeline: %d rows affected", n)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("updating DB: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit the transaction")
	}

	failed = false

	return true, nil
}

func (a *AggregateChunksTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (a *AggregateChunksTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "AggregateChunks",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 4 << 30,
		},
		MaxFailures: 1,
		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return a.schedule(context.Background(), taskFunc)
		}),
	}
}

func (a *AggregateChunksTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule
			var mid string
			var count int
			err := a.db.QueryRow(ctx, `SELECT id, COUNT(*) AS total_chunks
											FROM market_mk20_deal_chunk
											GROUP BY id
											HAVING
											  COUNT(*) = COUNT(*) FILTER (
												WHERE complete = TRUE
												  AND finalize = TRUE
												  AND finalize_task_id IS NULL
												  AND ref_id IS NOT NULL
											  )
											ORDER BY id
											LIMIT 1;`).Scan(&mid, &count)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("getting next task to schedule: %w", err)
			}
			if mid == "" {
				return false, xerrors.Errorf("no id for tasks to schedule")
			}

			n, err := tx.Exec(`UPDATE market_mk20_deal_chunk SET finalize_task_id = $1 
                              WHERE id = $2 
                                AND complete = TRUE 
                                AND finalize = TRUE 
                                AND finalize_task_id IS NULL
                                AND ref_id IS NOT NULL`, id, mid)
			if err != nil {
				return false, xerrors.Errorf("updating chunk finalize task: %w", err)
			}
			if n != count {
				return false, xerrors.Errorf("expected to update %d rows: %d rows affected", count, n)
			}
			stop = false
			return true, nil
		})
	}
	return nil
}

func (a *AggregateChunksTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&AggregateChunksTask{})
var _ harmonytask.TaskInterface = &AggregateChunksTask{}
