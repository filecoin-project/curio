package indexing

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/market/indexstore"
)

type PDPIndexingV0Task struct {
	db                *harmonydb.DB
	indexStore        *indexstore.IndexStore
	cpr               *cachedreader.CachedPieceReader
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
	max               taskhelp.Limiter
}

func NewPDPV0IndexingTask(db *harmonydb.DB, indexStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *PDPIndexingV0Task {

	return &PDPIndexingV0Task{
		db:                db,
		indexStore:        indexStore,
		cpr:               cpr,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
		max:               max,
	}
}

func (P *PDPIndexingV0Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		ID        int64               `db:"id"`
		PieceCID  string              `db:"piece_cid"`
		PieceSize abi.PaddedPieceSize `db:"piece_padded_size"`
		RawSize   uint64              `db:"piece_raw_size"`
	}

	err = P.db.Select(ctx, &tasks, `SELECT
										pr.id,
										pr.piece_cid,
										pp.piece_padded_size,
										pp.piece_raw_size
									FROM
										pdp_piecerefs pr
									JOIN parked_piece_refs ppr ON pr.piece_ref = ppr.ref_id
									JOIN parked_pieces pp ON ppr.piece_id = pp.id
									WHERE indexing_task_id = $1 
									  AND needs_indexing = TRUE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting PDP pending indexing tasks: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("incorrect rows for pending indexing tasks: %d", len(tasks))
	}
	task := tasks[0]

	pcid, err := cid.Parse(task.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	pcid2, err := commcid.PieceCidV2FromV1(pcid, task.RawSize)
	if err != nil {
		return false, xerrors.Errorf("converting piece CID to v2: %w", err)
	}

	hasIndex, err := P.indexStore.CheckHasPiece(ctx, pcid2)
	if err != nil {
		return false, xerrors.Errorf("checking if piece is already indexed: %w", err)
	}
	if !hasIndex {
		hasIndex, err = P.indexStore.CheckHasPiece(ctx, pcid)
		if err != nil {
			return false, xerrors.Errorf("checking if piece is already indexed: %w", err)
		}
	}
	if hasIndex {
		// Piece already indexed so either:
		// 1. A previous job with same commp indexed and completed IPNI
		// 2. A parallel job with same commp indexed and will/has set needs_ipni
		//
		// In both cases we do not need to index or publish new advertisements to IPNI
		err = P.recordCompletion(ctx, taskID, task.ID, false)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// Fetch with pcid2, it should automatically give us correct pieceReader
	reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid2, false)
	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	startTime := time.Now()

	// Note: These are not technically PDP config values, but we can share them with porep deal cfg
	chanSize := P.insertConcurrency * P.insertBatchSize

	recs := make(chan indexstore.Record, chanSize)
	var blocks int64

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool

	eg.Go(func() error {
		defer close(addFail)
		return P.indexStore.AddIndex(ctx, pcid, recs)
	})
	blocks, interrupted, err = IndexCAR(reader, 4<<20, recs, addFail)
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

	log.Infof("Indexing piece %d took %0.3f seconds", task.ID, time.Since(startTime).Seconds())

	err = P.recordCompletion(ctx, taskID, task.ID, true)
	if err != nil {
		return false, err
	}

	blocksPerSecond := float64(blocks) / time.Since(startTime).Seconds()
	log.Infow("Piece indexed", "piece_cid", task.PieceCID, "id", task.ID, "blocks", blocks, "blocks_per_second", blocksPerSecond)

	return true, nil
}

func (P *PDPIndexingV0Task) recordCompletion(ctx context.Context, taskID harmonytask.TaskID, id int64, needsIPNI bool) error {

	n, err := P.db.Exec(ctx, `UPDATE pdp_piecerefs SET needs_indexing = FALSE, needs_ipni = $3, indexing_task_id = NULL 
									WHERE id = $1 AND indexing_task_id = $2`, id, taskID, needsIPNI)
	if err != nil {
		return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store indexing success: updated %d rows", n)
	}

	return nil
}

func (P *PDPIndexingV0Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// We just accept all tasks
	// Note that this differs from markets v2 code which does a local storage check on the piece.
	//
	// If we are ever in an instance where the shared piece reader is expected to go to sector storage and will actually benefit from these local checks
	// then we should be using markets v2 code because then we'll be in a shared datasource world.  In the rare case where an SP is handling PDP and PoRep on
	// a cluster and just so happens to have a collision then the network overhead of moving a piece around will be small.
	return ids, nil
}

func (P *PDPIndexingV0Task) TypeDetails() harmonytask.TaskTypeDetails {
	// RAM: bufio reader, gocql batches, fr32/read path buffers, connection pools.
	const indexingTaskRAM = 128 << 20 // 128 MiB

	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_Indexing",
		Cost: resources.Resources{
			Cpu: 0, // I/O bound (storage read, CQL write), not CPU bound
			Ram: indexingTaskRAM,
		},
		Max:         P.max,
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return P.schedule(context.Background(), taskFunc)
		}),
	}
}

func (P *PDPIndexingV0Task) schedule(_ context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID int64 `db:"id"`
			}

			err := tx.Select(&pendings, `SELECT id FROM pdp_piecerefs 
            										WHERE indexing_task_id IS NULL
            										AND needs_indexing = TRUE
													ORDER BY created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting PDP pending indexing tasks: %w", err)
			}

			if len(pendings) == 0 {
				log.Debug("No pending PDP indexing tasks found")
				return false, nil
			}

			pending := pendings[0]
			n, err := tx.Exec(`UPDATE pdp_piecerefs SET indexing_task_id = $1
                             WHERE indexing_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating PDP indexing task id: %w", err)
			}
			if n == 0 {
				return false, nil // Another task already claimed this piece
			}
			log.Debugf("PDP indexing task scheduled for pending indexing task %d", pending.ID)

			stop = false
			return true, nil
		})
	}

	return nil
}

func (P *PDPIndexingV0Task) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPIndexingV0Task{}
var _ = harmonytask.Reg(&PDPIndexingV0Task{})
