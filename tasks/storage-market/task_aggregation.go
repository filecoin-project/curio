package storage_market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net/url"
	"strconv"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

type AggregateDealTask struct {
	sm   *CurioStorageDealMarket
	db   *harmonydb.DB
	sc   *ffi.SealCalls
	stor paths.StashStore
	api  headAPI
}

func NewAggregateTask(sm *CurioStorageDealMarket, db *harmonydb.DB, sc *ffi.SealCalls, stor paths.StashStore, api headAPI) *AggregateDealTask {
	return &AggregateDealTask{
		sm:   sm,
		db:   db,
		sc:   sc,
		stor: stor,
		api:  api,
	}
}

func (a *AggregateDealTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var pieces []struct {
		Pcid        string `db:"piece_cid"`
		Psize       int64  `db:"piece_size"`
		RawSize     int64  `db:"raw_size"`
		URL         string `db:"url"`
		ID          string `db:"id"`
		SpID        int64  `db:"sp_id"`
		AggrIndex   int    `db:"aggr_index"`
		Aggregated  bool   `db:"aggregated"`
		Aggregation int    `db:"deal_aggregation"`
	}

	err = a.db.Select(ctx, &pieces, `
										SELECT
										    piece_cid, 
											piece_size,
											raw_size,
											url, 
											id, 
											sp_id, 
											aggr_index,
											aggregated,
											deal_aggregation
										FROM 
											market_mk20_pipeline 
										WHERE 
											agg_task_id = $1 ORDER BY aggr_index ASC`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting piece details: %w", err)
	}

	if len(pieces) == 0 {
		return false, xerrors.Errorf("no pieces to aggregate for task %d", taskID)
	}

	if len(pieces) == 1 {
		n, err := a.db.Exec(ctx, `UPDATE market_mk20_pipeline SET aggregated = TRUE, agg_task_id = NULL 
                                   WHERE id = $1 
                                     AND agg_task_id = $2`, pieces[0].ID, taskID)
		if err != nil {
			return false, xerrors.Errorf("updating aggregated piece details in DB: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row updated, got %d", n)
		}
		log.Infof("skipping aggregation as deal %s only has 1 piece for task %s", pieces[0].ID, taskID)
		return true, nil
	}

	id := pieces[0].ID
	spid := pieces[0].SpID

	ID, err := ulid.Parse(id)
	if err != nil {
		return false, xerrors.Errorf("parsing deal ID: %w", err)
	}

	deal, err := mk20.DealFromDB(ctx, a.db, ID)
	if err != nil {
		return false, xerrors.Errorf("getting deal details from DB: %w", err)
	}

	pi, err := deal.PieceInfo()
	if err != nil {
		return false, xerrors.Errorf("getting piece info: %w", err)
	}

	var pinfos []abi.PieceInfo
	var readers []io.Reader

	var refIDs []int64

	for _, piece := range pieces {
		if piece.Aggregated {
			return false, xerrors.Errorf("piece %s for deal %s already aggregated for task %d", piece.Pcid, piece.ID, taskID)
		}
		if piece.Aggregation != 1 {
			return false, xerrors.Errorf("incorrect aggregation value for piece %s for deal %s for task %d", piece.Pcid, piece.ID, taskID)
		}
		if piece.ID != id || piece.SpID != spid {
			return false, xerrors.Errorf("piece details do not match")
		}
		goUrl, err := url.Parse(piece.URL)
		if err != nil {
			return false, xerrors.Errorf("parsing data URL: %w", err)
		}
		if goUrl.Scheme != "pieceref" {
			return false, xerrors.Errorf("invalid data URL scheme: %s", goUrl.Scheme)
		}

		var reader io.Reader // io.ReadCloser is not supported by padreader
		var closer io.Closer

		refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
		if err != nil {
			return false, xerrors.Errorf("parsing piece reference number: %w", err)
		}

		// get pieceID
		var pieceID []struct {
			PieceID storiface.PieceNumber `db:"piece_id"`
		}
		err = a.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
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

		closer = pr
		reader = pr
		defer func() {
			_ = closer.Close()
		}()

		pcid, err := cid.Parse(piece.Pcid)
		if err != nil {
			return false, xerrors.Errorf("parsing piece cid: %w", err)
		}

		pinfos = append(pinfos, abi.PieceInfo{
			Size:     abi.PaddedPieceSize(piece.Psize),
			PieceCID: pcid,
		})

		readers = append(readers, io.LimitReader(reader, piece.RawSize))
		refIDs = append(refIDs, refNum)
	}

	_, aggregatedRawSize, err := datasegment.ComputeDealPlacement(pinfos)
	if err != nil {
		return false, xerrors.Errorf("computing aggregated piece size: %w", err)
	}

	overallSize := abi.PaddedPieceSize(aggregatedRawSize)
	// we need to make this the 'next' power of 2 in order to have space for the index
	next := 1 << (64 - bits.LeadingZeros64(uint64(overallSize+256)))

	aggr, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), pinfos)
	if err != nil {
		return false, xerrors.Errorf("creating aggregate: %w", err)
	}

	outR, err := aggr.AggregateObjectReader(readers)
	if err != nil {
		return false, xerrors.Errorf("aggregating piece readers: %w", err)
	}

	var parkedPieceID, pieceRefID int64
	var pieceParked bool

	comm, err := a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Check if we already have the piece, if found then verify access and skip rest of the processing
		var pid int64
		err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2 AND long_term = TRUE`, pi.PieceCIDV1.String(), pi.Size).Scan(&pid)
		if err == nil {
			// If piece exists then check if we can access the data
			pr, err := a.sc.PieceReader(ctx, storiface.PieceNumber(pid))
			if err != nil {
				// If piece does not exist then we will park it otherwise fail here
				if !errors.Is(err, storiface.ErrSectorNotFound) {
					// We should fail here because any subsequent operation which requires access to data will also fail
					// till this error is fixed
					return false, fmt.Errorf("failed to get piece reader: %w", err)
				}
			}
			defer pr.Close()
			pieceParked = true
			parkedPieceID = pid
		} else {
			if !errors.Is(err, pgx.ErrNoRows) {
				return false, fmt.Errorf("failed to check if piece already exists: %w", err)
			}
			// If piece does not exist then let's create one
			err = tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
            VALUES ($1, $2, $3, TRUE, TRUE) RETURNING id`,
				pi.PieceCIDV1.String(), pi.Size, pi.RawSize).Scan(&parkedPieceID)
			if err != nil {
				return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
			}
		}

		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, "/Aggregate").Scan(&pieceRefID)
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

	// Clean up piece park tables in case of failure
	// TODO: Figure out if there is a race condition with cleanup task
	defer func() {
		if failed {
			_, ferr := a.db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, pieceRefID)
			if err != nil {
				log.Errorf("failed to delete parked_piece_refs entry: %w", ferr)
			}
		}
	}()

	// Write piece if not already complete
	if !pieceParked {
		upi, _, err := a.sc.WriteUploadPiece(ctx, storiface.PieceNumber(parkedPieceID), int64(pi.RawSize), outR, storiface.PathStorage, true)
		if err != nil {
			return false, xerrors.Errorf("writing aggregated piece data to storage: %w", err)
		}

		if !upi.PieceCID.Equals(pi.PieceCIDV1) {
			return false, xerrors.Errorf("commP mismatch calculated %s and supplied %s", upi.PieceCID.String(), pi.PieceCIDV1.String())
		}

		if upi.Size != pi.Size {
			return false, xerrors.Errorf("commP size mismatch calculated %d and supplied %d", upi.Size, pi.Size)
		}
	}

	comm, err = a.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		pieceIDUrl := url.URL{
			Scheme: "pieceref",
			Opaque: fmt.Sprintf("%d", pieceRefID),
		}

		// Replace the pipeline piece with a new aggregated piece
		_, err = tx.Exec(`DELETE FROM market_mk20_pipeline WHERE id = $1`, id)
		if err != nil {
			return false, fmt.Errorf("failed to delete pipeline pieces: %w", err)
		}

		_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = ANY($1) AND long_term = FALSE`, refIDs)
		if err != nil {
			return false, fmt.Errorf("failed to delete parked_piece_refs entries: %w", err)
		}

		var rev mk20.RetrievalV1
		if deal.Products.RetrievalV1 != nil {
			rev = *deal.Products.RetrievalV1
		}

		ddo := deal.Products.DDOV1
		data := deal.Data

		var allocationID interface{}
		if ddo.AllocationId != nil {
			allocationID = *ddo.AllocationId
		} else {
			allocationID = nil
		}

		n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size, raw_size, url, 
            offline, indexing, announce, allocation_id, duration, 
            piece_aggregation, deal_aggregation, started, downloaded, after_commp, aggregated) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, TRUE, TRUE, TRUE, TRUE)`,
			id, spid, ddo.ContractAddress, deal.Client.String(), deal.Data.PieceCID.String(), pi.PieceCIDV1.String(), pi.Size, pi.RawSize, pieceIDUrl.String(),
			false, rev.Indexing, rev.AnnouncePayload, allocationID, ddo.Duration,
			data.Format.Aggregate.Type, data.Format.Aggregate.Type)
		if err != nil {
			return false, xerrors.Errorf("inserting aggregated piece in mk20 pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("inserting aggregated piece in mk20 pipeline: %d rows affected", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("saving aggregated piece details to DB: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit the transaction")
	}

	failed = false

	return true, nil
}

func (a *AggregateDealTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// If no local pieceRef was found then just return first TaskID
	return &ids[0], nil
}

func (a *AggregateDealTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "AggregateDeals",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 4 << 30,
		},
		MaxFailures: 3,
	}
}

func (a *AggregateDealTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	a.sm.adders[pollerAggregate].Set(taskFunc)
}

var _ = harmonytask.Reg(&AggregateDealTask{})
var _ harmonytask.TaskInterface = &AggregateDealTask{}
