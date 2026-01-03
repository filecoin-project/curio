package indexing

import (
	"context"
	"errors"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

type PDPIndexingTask struct {
	db                *harmonydb.DB
	indexStore        *indexstore.IndexStore
	cpr               *cachedreader.CachedPieceReader
	sc                *ffi.SealCalls
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
	max               taskhelp.Limiter
}

func NewPDPIndexingTask(db *harmonydb.DB, sc *ffi.SealCalls, indexStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *PDPIndexingTask {

	return &PDPIndexingTask{
		db:                db,
		indexStore:        indexStore,
		cpr:               cpr,
		sc:                sc,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
		max:               max,
	}
}

func (P *PDPIndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		ID         string `db:"id"`
		PieceCIDV2 string `db:"piece_cid_v2"`
		PieceRef   int64  `db:"piece_ref"`
		Indexing   bool   `db:"indexing"`
	}

	err = P.db.Select(ctx, &tasks, `SELECT id, piece_cid_v2, piece_ref, indexing FROM pdp_pipeline WHERE indexing_task_id = $1 AND indexed = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting PDP pending indexing tasks: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("incorrect rows for pending indexing tasks: %d", len(tasks))
	}

	task := tasks[0]

	pcid2, err := cid.Parse(task.PieceCIDV2)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid2)
	if err != nil {
		return false, xerrors.Errorf("getting piece info: %w", err)
	}

	var indexed bool
	err = P.db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1 and piece_size = $2`, pi.PieceCIDV1.String(), pi.Size).Scan(&indexed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("checking if piece %s is already indexed: %w", task.PieceCIDV2, err)
	}

	id, err := ulid.Parse(task.ID)
	if err != nil {
		return false, xerrors.Errorf("parsing task id: %w", err)
	}

	deal, err := mk20.DealFromDB(ctx, P.db, id)
	if err != nil {
		return false, xerrors.Errorf("getting deal from db: %w", err)
	}

	var subPieces []mk20.DataSource
	var byteData bool

	if deal.Data.Format.Aggregate != nil {
		if deal.Data.Format.Aggregate.Type > 0 {
			var found bool
			if len(deal.Data.Format.Aggregate.Sub) > 0 {
				subPieces = deal.Data.Format.Aggregate.Sub
				found = true
			}
			if len(deal.Data.SourceAggregate.Pieces) > 0 {
				subPieces = deal.Data.SourceAggregate.Pieces
				found = true
			}
			if !found {
				return false, xerrors.Errorf("no sub pieces for aggregate PDP deal")
			}
		}
	}

	if deal.Data.Format.Raw != nil {
		byteData = true
	}

	if indexed || !task.Indexing || byteData {
		err = P.recordCompletion(ctx, taskID, task.ID, pi.PieceCIDV1.String(), int64(pi.Size), int64(pi.RawSize), task.PieceRef, false)
		if err != nil {
			return false, err
		}
		log.Infow("Piece already indexed or should not be indexed", "piece_cid", task.PieceCIDV2, "indexed", indexed, "should_index", task.Indexing, "id", task.ID, "sp_id")

		return true, nil
	}

	reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid2, false)

	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	defer func() {
		_ = reader.Close()
	}()

	startTime := time.Now()

	dealCfg := P.cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan indexstore.Record, chanSize)
	var blocks int64

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool

	eg.Go(func() error {
		defer close(addFail)
		return P.indexStore.AddIndex(ctx, pcid2, recs)
	})

	var aggidx map[cid.Cid][]indexstore.Record

	if len(subPieces) > 0 {
		blocks, aggidx, interrupted, err = IndexAggregate(pcid2, reader, pi.Size, subPieces, recs, addFail)
	} else {
		blocks, interrupted, err = IndexCAR(reader, 4<<20, recs, addFail)
	}

	if err != nil {
		// Indexing itself failed, stop early
		close(recs) // still safe to close, AddIndex will exit on channel close
		// wait for AddIndex goroutine to finish cleanly
		_ = eg.Wait()
		return false, xerrors.Errorf("indexing failed: %w", err)
	}

	// Close the channel
	close(recs)

	// Wait till AddIndex is finished
	err = eg.Wait()
	if err != nil {
		return false, xerrors.Errorf("adding index to DB (interrupted %t): %w", interrupted, err)
	}

	log.Infof("Indexing deal %s took %0.3f seconds", task.ID, time.Since(startTime).Seconds())

	// Save aggregate index if present
	for k, v := range aggidx {
		if len(v) > 0 {
			err = P.indexStore.InsertAggregateIndex(ctx, k, v)
			if err != nil {
				return false, xerrors.Errorf("inserting aggregate index: %w", err)
			}
		}
	}

	err = P.recordCompletion(ctx, taskID, task.ID, pi.PieceCIDV1.String(), int64(pi.Size), int64(pi.RawSize), task.PieceRef, true)
	if err != nil {
		return false, err
	}

	blocksPerSecond := float64(blocks) / time.Since(startTime).Seconds()
	log.Infow("Piece indexed", "piece_cid", task.PieceCIDV2, "id", task.ID, "blocks", blocks, "blocks_per_second", blocksPerSecond)

	return true, nil
}

func (P *PDPIndexingTask) recordCompletion(ctx context.Context, taskID harmonytask.TaskID, id, PieceCID string, size, rawSize, pieceRef int64, indexed bool) error {
	comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		_, err = tx.Exec(`SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			id, PieceCID, false, -1, -1, nil, size, rawSize, indexed, pieceRef, false, 0)
		if err != nil {
			return false, xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", id, err)
		}

		if P.cfg.Market.StorageMarketConfig.IPNI.Disable {
			n, err := P.db.Exec(ctx, `UPDATE pdp_pipeline SET indexed = TRUE, indexing_task_id = NULL, 
                                     complete = TRUE WHERE id = $1 AND indexing_task_id = $2`, id, taskID)
			if err != nil {
				return false, xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		} else {
			n, err := tx.Exec(`UPDATE pdp_pipeline SET indexed = TRUE, indexing_task_id = NULL 
                                 WHERE id = $1 AND indexing_task_id = $2`, id, taskID)
			if err != nil {
				return false, xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("committing transaction: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
}

func (P *PDPIndexingTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for x, id := range ids {
		indIDs[x] = int64(id)
	}

	if storiface.FTPiece != 32 {
		panic("storiface.FTPiece != 32")
	}

	ls, err := P.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	localIDs := make([]string, len(ls))
	for x, l := range ls {
		localIDs[x] = string(l.ID)
	}

	var resultTaskID harmonytask.TaskID

	// Single query to resolve storage locations and filter for acceptable tasks
	err = P.db.QueryRow(ctx, `
		SELECT indexing_task_id
		FROM pdp_pipeline p
		LEFT JOIN parked_piece_refs ppr ON p.piece_ref = ppr.ref_id
		LEFT JOIN sector_location sl ON sl.sector_num = ppr.piece_id 
		  AND sl.miner_id = 0 
		  AND sl.sector_filetype = 32
		WHERE p.indexing_task_id = ANY($1::bigint[])
		  AND (p.indexing = FALSE OR sl.storage_id = ANY($2::text[]))
		LIMIT 1`, indIDs, localIDs).Scan(&resultTaskID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, xerrors.Errorf("pdp can accept batch query: %w", err)
	}

	return &resultTaskID, nil
}

func (P *PDPIndexingTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPIndexing",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: uint64(P.insertBatchSize * P.insertConcurrency * 56 * 2),
		},
		Max:         P.max,
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return P.schedule(context.Background(), taskFunc)
		}),
	}
}

func (P *PDPIndexingTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID string `db:"id"`
			}

			err := tx.Select(&pendings, `SELECT id FROM pdp_pipeline 
            										WHERE after_save_cache = TRUE
            										AND indexing_task_id IS NULL
            										AND indexed = FALSE
													ORDER BY indexing_created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting PDP pending indexing tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			pending := pendings[0]
			_, err = tx.Exec(`UPDATE pdp_pipeline SET indexing_task_id = $1 
                             WHERE indexing_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating PDP indexing task id: %w", err)
			}

			stop = false
			return true, nil
		})
	}

	return nil
}

func (P *PDPIndexingTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPIndexingTask{}
var _ = harmonytask.Reg(&PDPIndexingTask{})
