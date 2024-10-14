package indexing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
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
	pieceProvider     *pieceprovider.PieceProvider
	sc                *ffi.SealCalls
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
}

func NewIndexingTask(db *harmonydb.DB, sc *ffi.SealCalls, indexStore *indexstore.IndexStore, pieceProvider *pieceprovider.PieceProvider, cfg *config.CurioConfig) *IndexingTask {

	return &IndexingTask{
		db:                db,
		indexStore:        indexStore,
		pieceProvider:     pieceProvider,
		sc:                sc,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
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
}

func (i *IndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var tasks []itask

	ctx := context.Background()

	err = i.db.Select(ctx, &tasks, `SELECT 
											uuid, 
											sp_id, 
											sector,
											piece_cid, 
											piece_size, 
											sector_offset,
											reg_seal_proof,
											raw_size,
											should_index,
											announce
										FROM 
											market_mk12_deal_pipeline
										WHERE 
											indexing_task_id = $1;`, taskID)
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
	if err != nil {
		return false, xerrors.Errorf("checking if piece is already indexed: %w", err)
	}

	// Return early if already indexed or should not be indexed
	if indexed || !task.ShouldIndex {
		err = i.recordCompletion(ctx, task, taskID, false)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	unsealed, err := i.pieceProvider.IsUnsealed(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: task.Sector,
		},
		ProofType: task.Proof,
	}, storiface.UnpaddedByteIndex(task.Offset), task.Size.Unpadded())
	if err != nil {
		return false, xerrors.Errorf("checking if sector is unsealed :%w", err)
	}

	if !unsealed {
		return false, xerrors.Errorf("sector %d for miner %d is not unsealed", task.Sector, task.SpID)
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
	}, storiface.UnpaddedByteIndex(abi.PaddedPieceSize(task.Offset).Unpadded()), task.Size.Unpadded(), pieceCid)

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
	blockReader, err := carv2.NewBlockReader(reader, opts...)
	if err != nil {
		return false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		serr := i.indexStore.AddIndex(ctx, pieceCid, recs)
		if serr != nil {
			return xerrors.Errorf("adding index to DB: %w", err)
		}
		return nil
	})

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		recs <- indexstore.Record{
			Cid: blockMetadata.Cid,
			OffsetSize: indexstore.OffsetSize{
				Offset: blockMetadata.SourceOffset,
				Size:   blockMetadata.Size,
			},
		}
		blockMetadata, err = blockReader.SkipNext()
	}
	if !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("generating index for piece: %w", err)
	}

	// Close the channel
	close(recs)

	// Wait till AddIndex is finished
	err = eg.Wait()
	if err != nil {
		return false, xerrors.Errorf("adding index to DB: %w", err)
	}

	log.Infof("Indexing deal %s took %0.3f seconds", task.UUID, time.Since(startTime).Seconds())

	err = i.recordCompletion(ctx, task, taskID, true)
	if err != nil {
		return false, err
	}

	return true, nil
}

// recordCompletion add the piece metadata and piece deal to the DB and
// records the completion of an indexing task in the database
func (i *IndexingTask) recordCompletion(ctx context.Context, task itask, taskID harmonytask.TaskID, indexed bool) error {
	_, err := i.db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		task.UUID, task.PieceCid, true, task.SpID, task.Sector, task.Offset, task.Size, task.RawSize, indexed)
	if err != nil {
		return xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", task.UUID, err)
	}

	// If IPNI is disabled then mark deal as complete otherwise just mark as indexed
	if i.cfg.Market.StorageMarketConfig.IPNI.Disable {
		n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE, complete = TRUE WHERE uuid = $1`, task.UUID)
		if err != nil {
			return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("store indexing success: updated %d rows", n)
		}
	} else {
		n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE WHERE uuid = $1`, task.UUID)
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
	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"indexing_task_id"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector"`
		StorageID    string             `db:"storage_id"`
	}

	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	err := i.db.Select(ctx, &tasks, `
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

	acceptables := map[harmonytask.TaskID]bool{}

	for _, t := range ids {
		acceptables[t] = true
	}

	for _, t := range tasks {
		if _, ok := acceptables[t.TaskID]; !ok {
			continue
		}

		for _, l := range ls {
			if string(l.ID) == t.StorageID {
				return &t.TaskID, nil
			}
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
		Max:         taskhelp.Max(4),
		MaxFailures: 3,
		IAmBored: passcall.Every(10*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
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

			err := i.db.Select(ctx, &pendings, `SELECT uuid FROM market_mk12_deal_pipeline 
            										WHERE sealed = TRUE
            										AND indexing_task_id IS NULL 
													ORDER BY indexing_created_at ASC;`)
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
