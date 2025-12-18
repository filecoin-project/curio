package indexing

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/market/indexstore"
)

type PDPv0IndexingTask struct {
	db                *harmonydb.DB
	indexStore        *indexstore.IndexStore
	cpr               *cachedreader.CachedPieceReader
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
	max               taskhelp.Limiter
}

func NewPDPv0IndexingTask(db *harmonydb.DB, indexStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *PDPv0IndexingTask {

	return &PDPv0IndexingTask{
		db:                db,
		indexStore:        indexStore,
		cpr:               cpr,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
		max:               max,
	}
}

func (P *PDPv0IndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		ID       int64  `db:"id"`
		PieceCID string `db:"piece_cid"`
		PieceRef int64  `db:"piece_ref"`
	}

	err = P.db.Select(ctx, &tasks, `SELECT id, piece_cid, piece_ref FROM pdp_piecerefs WHERE indexing_task_id = $1 AND needs_indexing = TRUE`, taskID)
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

	hasIndex, err := P.indexStore.CheckHasPiece(ctx, pcid)
	if err != nil {
		return false, xerrors.Errorf("checking if piece is already indexed: %w", err)
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

	reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid, false)
	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	startTime := time.Now()

	// Note: These are not technically PDP config values, but we can share them with porep deal cfg
	dealCfg := P.cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan indexstore.Record, chanSize)
	var blocks int64

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool

	eg.Go(func() error {
		defer close(addFail)
		return P.indexStore.AddIndex(ctx, pcid, recs)
	})
	blocks, interrupted, err = PDPV0IndexCAR(reader, 4<<20, recs, addFail)
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

func (P *PDPv0IndexingTask) recordCompletion(ctx context.Context, taskID harmonytask.TaskID, id int64, needsIPNI bool) error {
	comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {

		n, err := P.db.Exec(ctx, `UPDATE pdp_piecerefs SET needs_indexing = FALSE, needs_ipni = $3, indexing_task_id = NULL 
									WHERE id = $1 AND indexing_task_id = $2`, id, taskID, needsIPNI)
		if err != nil {
			return false, xerrors.Errorf("store indexing success: updating pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("store indexing success: updated %d rows", n)
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

func (P *PDPv0IndexingTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// We just accept all tasks
	// Note that this differs from markets v2 code which does a local storage check on the piece.
	//
	// If we are ever in an instance where the shared piece reader is expected to go to sector storage and will actually benefit from these local checks
	// then we should be using markets v2 code because then we'll be in a shared datasource world.  In the rare case where an SP is handling PDP and PoRep on
	// a cluster and just so happens to have a collision then the network overhead of moving a piece around will be small.
	id := ids[0]
	return &id, nil
}

func (P *PDPv0IndexingTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_Indexing",
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

func (P *PDPv0IndexingTask) schedule(_ context.Context, taskFunc harmonytask.AddTaskFunc) error {
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
			_, err = tx.Exec(`UPDATE pdp_piecerefs SET indexing_task_id = $1 
                             WHERE indexing_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating PDP indexing task id: %w", err)
			}
			log.Debugf("PDP indexing task scheduled for pending indexing task %d", pending.ID)

			stop = false
			return true, nil
		})
	}

	return nil
}

func (P *PDPv0IndexingTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPIndexingTask{}
var _ = harmonytask.Reg(&PDPIndexingTask{})

func PDPV0IndexCAR(r io.Reader, buffSize int, recs chan<- indexstore.Record, addFail <-chan struct{}) (int64, bool, error) {
	// ZeroLengthSectionAsEOF is not strictly needed here as it exists for the PoRep case where
	// padding pieces with zero bytes to get them to be a larger size is reasonable. This isn't
	// expected to be the case with PDP, but we'll stay consistent.
	blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(r, buffSize), carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return 0, false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	var blocks int64
	var interrupted bool

	for {
		blockMetadata, err := blockReader.SkipNext()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return blocks, interrupted, fmt.Errorf("generating index for piece: %w", err)
		}

		blocks++

		select {
		case recs <- indexstore.Record{
			Cid:    blockMetadata.Cid,
			Offset: blockMetadata.SourceOffset,
			Size:   blockMetadata.Size,
		}:
		case <-addFail:
			interrupted = true
		}

		if interrupted {
			break
		}
	}

	return blocks, interrupted, nil
}
