package indexing

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

var log = logging.Logger("indexing")

type IndexingTask struct {
	db                *harmonydb.DB
	indexStore        *indexstore.IndexStore
	pieceProvider     *pieceprovider.SectorReader
	sc                *ffi.SealCalls
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
	max               taskhelp.Limiter
}

func NewIndexingTask(db *harmonydb.DB, sc *ffi.SealCalls, indexStore *indexstore.IndexStore, pieceProvider *pieceprovider.SectorReader, cfg *config.CurioConfig, max taskhelp.Limiter) *IndexingTask {

	return &IndexingTask{
		db:                db,
		indexStore:        indexStore,
		pieceProvider:     pieceProvider,
		sc:                sc,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
		max:               max,
	}
}

type itask struct {
	UUID        string                  `db:"uuid"`
	SpID        int64                   `db:"sp_id"`
	Sector      abi.SectorNumber        `db:"sector"`
	Proof       abi.RegisteredSealProof `db:"reg_seal_proof"`
	PieceCid    string                  `db:"piece_cid"`
	Size        abi.PaddedPieceSize     `db:"piece_size"`
	Offset      int64                   `db:"sector_offset"`
	RawSize     int64                   `db:"raw_size"`
	ShouldIndex bool                    `db:"should_index"`
	Announce    bool                    `db:"announce"`
	ChainDealId abi.DealID              `db:"chain_deal_id"`
	IsDDO       bool                    `db:"is_ddo"`
}

func (i *IndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var tasks []itask

	ctx := context.Background()

	err = i.db.Select(ctx, &tasks, `SELECT 
											p.uuid, 
											p.sp_id, 
											p.sector,
											p.piece_cid, 
											p.piece_size, 
											p.sector_offset,
											p.reg_seal_proof,
											p.raw_size,
											p.should_index,
											p.announce,
											p.is_ddo,
											COALESCE(d.chain_deal_id, 0) AS chain_deal_id  -- If NULL, return 0
										FROM 
											market_mk12_deal_pipeline p
										LEFT JOIN 
											market_mk12_deals d 
											ON p.uuid = d.uuid AND p.sp_id = d.sp_id
										LEFT JOIN 
											market_direct_deals md 
											ON p.uuid = md.uuid AND p.sp_id = md.sp_id
										WHERE 
											p.indexing_task_id = $1;
										;`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting indexing params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	task := tasks[0]

	// Check if piece is already indexed
	var indexed bool
	err = i.db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1`, task.PieceCid).Scan(&indexed)
	if err != nil && err != pgx.ErrNoRows {
		return false, xerrors.Errorf("checking if piece %s is already indexed: %w", task.PieceCid, err)
	}

	// Return early if already indexed or should not be indexed
	if indexed || !task.ShouldIndex {
		err = i.recordCompletion(ctx, task, taskID, false)
		if err != nil {
			return false, err
		}
		log.Infow("Piece already indexed or should not be indexed", "piece_cid", task.PieceCid, "indexed", indexed, "should_index", task.ShouldIndex, "uuid", task.UUID, "sp_id", task.SpID, "sector", task.Sector)

		return true, nil
	}

	pieceCid, err := cid.Parse(task.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	reader, err := i.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: task.Sector,
		},
		ProofType: task.Proof,
	}, storiface.PaddedByteIndex(task.Offset).Unpadded(), task.Size.Unpadded(), pieceCid)

	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	defer reader.Close()

	startTime := time.Now()

	dealCfg := i.cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan indexstore.Record, chanSize)

	//recs := make([]indexstore.Record, 0, chanSize)
	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(reader, 4<<20), opts...)
	if err != nil {
		return false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool
	var blocks int64
	start := time.Now()

	eg.Go(func() error {
		defer close(addFail)

		serr := i.indexStore.AddIndex(ctx, pieceCid, recs)
		if serr != nil {
			return xerrors.Errorf("adding index to DB: %w", serr)
		}
		return nil
	})

	blockMetadata, err := blockReader.SkipNext()
loop:
	for err == nil {
		blocks++

		select {
		case recs <- indexstore.Record{
			Cid:    blockMetadata.Cid,
			Offset: blockMetadata.Offset,
			Size:   blockMetadata.Size,
		}:
		case <-addFail:
			interrupted = true
			break loop
		}
		blockMetadata, err = blockReader.SkipNext()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("generating index for piece: %w", err)
	}

	// Close the channel
	close(recs)

	// Wait till AddIndex is finished
	err = eg.Wait()
	if err != nil {
		return false, xerrors.Errorf("adding index to DB (interrupted %t): %w", interrupted, err)
	}

	log.Infof("Indexing deal %s took %0.3f seconds", task.UUID, time.Since(startTime).Seconds())

	err = i.recordCompletion(ctx, task, taskID, true)
	if err != nil {
		return false, err
	}

	blocksPerSecond := float64(blocks) / time.Since(start).Seconds()
	log.Infow("Piece indexed", "piece_cid", task.PieceCid, "uuid", task.UUID, "sp_id", task.SpID, "sector", task.Sector, "blocks", blocks, "blocks_per_second", blocksPerSecond)

	return true, nil
}

// recordCompletion add the piece metadata and piece deal to the DB and
// records the completion of an indexing task in the database
func (i *IndexingTask) recordCompletion(ctx context.Context, task itask, taskID harmonytask.TaskID, indexed bool) error {
	_, err := i.db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		task.UUID, task.PieceCid, !task.IsDDO, task.SpID, task.Sector, task.Offset, task.Size, task.RawSize, indexed, false, task.ChainDealId)
	if err != nil {
		return xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", task.UUID, err)
	}

	// If IPNI is disabled then mark deal as complete otherwise just mark as indexed
	if i.cfg.Market.StorageMarketConfig.IPNI.Disable {
		n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE, indexing_task_id = NULL, 
                                     complete = TRUE WHERE uuid = $1 AND indexing_task_id = $2`, task.UUID, taskID)
		if err != nil {
			return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("store indexing success: updated %d rows", n)
		}
	} else {
		n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE, indexing_task_id = NULL 
                                 WHERE uuid = $1 AND indexing_task_id = $2`, task.UUID, taskID)
		if err != nil {
			return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("store indexing success: updated %d rows", n)
		}
	}

	return nil
}

func (i *IndexingTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for x, id := range ids {
		indIDs[x] = int64(id)
	}

	// Accept any task which should not be indexed as
	// it does not require storage access
	var id int64
	err := i.db.QueryRow(ctx, `SELECT indexing_task_id 
										FROM market_mk12_deal_pipeline 
										WHERE should_index = FALSE AND 
										      indexing_task_id = ANY ($1) ORDER BY indexing_task_id LIMIT 1`, indIDs).Scan(&id)
	if err == nil {
		ret := harmonytask.TaskID(id)
		return &ret, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("getting pending indexing task: %w", err)
	}

	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"indexing_task_id"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector"`
		StorageID    string             `db:"storage_id"`
	}

	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	err = i.db.Select(ctx, &tasks, `
		SELECT dp.indexing_task_id, dp.sp_id, dp.sector, l.storage_id FROM market_mk12_deal_pipeline dp
			INNER JOIN sector_location l ON dp.sp_id = l.miner_id AND dp.sector = l.sector_num
			WHERE dp.indexing_task_id = ANY ($1) AND l.sector_filetype = 1
`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks: %w", err)
	}

	ls, err := i.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	localStorageMap := make(map[string]bool, len(ls))
	for _, l := range ls {
		localStorageMap[string(l.ID)] = true
	}

	for _, t := range tasks {
		if found, ok := localStorageMap[t.StorageID]; ok && found {
			return &t.TaskID, nil
		}
	}

	return nil, nil
}

func (i *IndexingTask) TypeDetails() harmonytask.TaskTypeDetails {
	//dealCfg := i.cfg.Market.StorageMarketConfig
	//chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize * 56 // (56 = size of each index.Record)

	return harmonytask.TaskTypeDetails{
		Name: "Indexing",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: uint64(i.insertBatchSize * i.insertConcurrency * 56 * 2),
		},
		Max:         i.max,
		MaxFailures: 3,
		IAmBored: passcall.Every(30*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return i.schedule(context.Background(), taskFunc)
		}),
	}
}

func (i *IndexingTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				UUID string `db:"uuid"`
			}

			// Indexing job must be created for every deal to make sure piece details are inserted in DB
			// even if we don't want to index it. If piece is not supposed to be indexed then it will handled
			// by the Do()
			err := i.db.Select(ctx, &pendings, `SELECT uuid FROM market_mk12_deal_pipeline 
            										WHERE sealed = TRUE
            										AND indexing_task_id IS NULL
            										AND indexed = FALSE
													ORDER BY indexing_created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting pending indexing tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			pending := pendings[0]

			_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET indexing_task_id = $1 
                             WHERE indexing_task_id IS NULL AND uuid = $2`, id, pending.UUID)
			if err != nil {
				return false, xerrors.Errorf("updating indexing task id: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})
	}

	return nil
}

func (i *IndexingTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (i *IndexingTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	var spid string
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM market_mk12_deal_pipeline WHERE indexing_task_id = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err)
		return ""
	}
	return spid
}

var _ = harmonytask.Reg(&IndexingTask{})
var _ harmonytask.TaskInterface = &IndexingTask{}
