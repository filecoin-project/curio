package piece

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-commp-utils/writer"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/market/mk20"
)

type AggregateChunksTask struct {
	db     *harmonydb.DB
	stor   paths.StashStore
	remote *paths.Remote
	sc     *ffi.SealCalls
}

func NewAggregateChunksTask(db *harmonydb.DB, stor paths.StashStore, remote *paths.Remote, sc *ffi.SealCalls) *AggregateChunksTask {
	return &AggregateChunksTask{
		db:     db,
		stor:   stor,
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

	var isMk20 bool
	var id ulid.ULID
	var uid uuid.UUID
	uid, err = uuid.Parse(idStr)
	if err != nil {
		serr := err
		id, err = ulid.Parse(idStr)
		if err != nil {
			return false, xerrors.Errorf("parsing deal ID: %w, %w", serr, err)
		}
		isMk20 = true
	}

	var rawSize int64
	var pcid cid.Cid
	var psize abi.PaddedPieceSize
	var deal *mk20.Deal

	if isMk20 {
		deal, err = mk20.DealFromDB(ctx, a.db, id)
		if err != nil {
			return false, xerrors.Errorf("getting deal details: %w", err)
		}
		raw, err := deal.Data.RawSize()
		if err != nil {
			return false, xerrors.Errorf("getting deal raw size: %w", err)
		}
		rawSize = int64(raw)
		pcid = deal.Data.PieceCID
		psize = deal.Data.Size
	} else {
		rawSize = 4817498192 // TODO: Fix this for PDP
		fmt.Println(uid)
	}

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

	w := &writer.Writer{}

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitReader := io.LimitReader(rd, rawSize)

		multiWriter := io.MultiWriter(w, f)

		n, err := io.CopyBuffer(multiWriter, limitReader, make([]byte, writer.CommPBuf))
		if err != nil {
			return fmt.Errorf("failed to read and write aggregated piece data: %w", err)
		}

		if n != rawSize {
			return fmt.Errorf("number of bytes written to CommP writer %d not equal to the file size %d", n, rawSize)
		}

		return nil
	}

	stashID, err := a.stor.StashCreate(ctx, rawSize, writeFunc)
	if err != nil {
		return false, xerrors.Errorf("stashing aggregated piece data: %w", err)
	}

	calculatedCommp, err := w.Sum()
	if err != nil {
		return false, xerrors.Errorf("computing commP failed: %w", err)
	}

	if !calculatedCommp.PieceCID.Equals(pcid) {
		return false, xerrors.Errorf("commP mismatch calculated %s and supplied %s", calculatedCommp.PieceCID.String(), pcid.String())
	}

	if calculatedCommp.PieceSize != psize {
		return false, xerrors.Errorf("commP size mismatch calculated %d and supplied %d", calculatedCommp.PieceSize, psize)
	}

	stashUrl, err := a.stor.StashURL(stashID)
	if err != nil {
		return false, xerrors.Errorf("getting stash URL: %w", err)
	}
	stashUrl.Scheme = dealdata.CustoreScheme

	comm, err := a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var parkedPieceID int64

		err = tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
            VALUES ($1, $2, $3, TRUE) RETURNING id
        `, calculatedCommp.PieceCID.String(), calculatedCommp.PieceSize, rawSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		var pieceRefID int64
		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, stashUrl.String()).Scan(&pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}

		if isMk20 {
			n, err := tx.Exec(`INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids) VALUES ($1, $2, $3, $4)`,
				id.String(), deal.Data.PieceCID.String(), deal.Data.Size, []int64{pieceRefID})
			if err != nil {
				return false, xerrors.Errorf("inserting mk20 download pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("inserting mk20 download pipeline: %d rows affected", n)
			}

			spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
			if err != nil {
				return false, fmt.Errorf("getting provider ID: %w", err)
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

			n, err = tx.Exec(`INSERT INTO market_mk20_pipeline (
		           id, sp_id, contract, client, piece_cid,
		           piece_size, raw_size, offline, indexing, announce,
		           allocation_id, duration, piece_aggregation, started, after_commp)
		       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, TRUE, TRUE)`,
				dealID, spid, ddo.ContractAddress, ddo.Client.String(), dealdata.PieceCID.String(),
				dealdata.Size, int64(dealdata.SourceHttpPut.RawSize), false, ddo.Indexing, ddo.AnnounceToIPNI,
				allocationID, ddo.Duration, aggregation)
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
		} else {
			return false, xerrors.Errorf("not implemented for PDP")
			// TODO: Do what is required for PDP
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("saving aggregated chunk details to DB: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit the transaction")
	}
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
		MaxFailures: 3,
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
