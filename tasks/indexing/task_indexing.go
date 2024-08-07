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
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/indexing/indexstore"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

var log = logging.Logger("indexing")

type IndexingTask struct {
	db                   *harmonydb.DB
	indexStore           *indexstore.IndexStore
	pieceProvider        *pieceprovider.PieceProvider
	maxCurrentForCluster int //TODO: Make this config
}

func NewIndexingTask(db *harmonydb.DB, indexStore *indexstore.IndexStore, pieceProvider *pieceprovider.PieceProvider) *IndexingTask {

	return &IndexingTask{
		db:            db,
		indexStore:    indexStore,
		pieceProvider: pieceProvider,
	}
}

type itask struct {
	UUID      string                  `db:"id"`
	SpID      int64                   `db:"sp_id"`
	Sector    abi.SectorNumber        `db:"sector_number"`
	Proof     abi.RegisteredSealProof `db:"reg_seal_proof"`
	PieceCid  string                  `db:"piece_cid"`
	Size      abi.PaddedPieceSize     `db:"piece_size"`
	CreatedAt time.Time               `db:"created_at"`
	Offset    int64                   `db:"piece_offset"`
	ChainID   abi.DealID              `db:"chain_deal_id"`
	RawSize   int64                   `db:"raw_size"`
}

func (i *IndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var tasks []itask

	ctx := context.Background()

	err = i.db.Select(ctx, &tasks, `SELECT 
											mit.id as id, 
											mit.sp_id as sp_id, 
											mit.sector_number as sector_number, 
											mit.piece_cid as piece_cid, 
											mit.piece_size as piece_size, 
											mit.created_at as created_at, 
											mit.piece_offset as piece_offset, 
											mit.reg_seal_proof as reg_seal_proof,
											md.chain_deal_id as chain_deal_id,
											mk.file_size as raw_size
										FROM 
											market_indexing_tasks mit
										JOIN 
											market_mk12_deal_pipeline mk ON mit.id = mk.uuid
										JOIN 
											market_mk12_deals md ON mk.id = md.uuid
										WHERE 
											mit.task_id = $1 
										ORDER BY 
											mit.created_at ASC;`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting indexing params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	task := tasks[0]

	// Check if piece is already indexed
	var indexed bool
	err = i.db.Select(ctx, &indexed, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1`, task.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("checking if piece is already indexed: %w", err)
	}

	// Return early if already indexed
	if indexed {
		err = i.recordCompletion(ctx, task, taskID)
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
	}, storiface.UnpaddedByteIndex(task.Offset), abi.UnpaddedPieceSize(task.Size), pieceCid)

	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	recs := make([]indexstore.Record, 0)
	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(reader, opts...)
	if err != nil {
		return false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		recs = append(recs, indexstore.Record{
			Cid: blockMetadata.Cid,
			OffsetSize: indexstore.OffsetSize{
				Offset: blockMetadata.SourceOffset,
				Size:   blockMetadata.Size,
			},
		})

		blockMetadata, err = blockReader.SkipNext()
	}
	if !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("generating index for piece: %w", err)
	}

	err = i.indexStore.AddIndex(ctx, pieceCid, recs)
	if err != nil {
		return false, xerrors.Errorf("adding index to DB: %w", err)
	}

	err = i.recordCompletion(ctx, task, taskID)
	if err != nil {
		return false, err
	}

	return true, nil
}

// recordCompletion add the piece metadata and piece deal to the DB and
// records the completion of an indexing task in the database
func (i *IndexingTask) recordCompletion(ctx context.Context, task itask, taskID harmonytask.TaskID) error {
	_, err := i.db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		task.UUID, task.PieceCid, true, false, task.ChainID, task.SpID, task.Sector, task.Offset, task.Size, task.RawSize)
	if err != nil {
		return xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", task.UUID, err)
	}

	_, err = i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE WHERE uuid = $1`, task.UUID)
	if err != nil {
		return xerrors.Errorf("failed to update deal pipeline for deal %s: %w", task.UUID, err)
	}

	n, err := i.db.Exec(ctx, `DELETE FROM market_indexing_tasks WHERE task_id = $1`, taskID)
	if err != nil {
		return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store indexing success: updated %d rows", n)
	}
	return nil
}

func (i *IndexingTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (i *IndexingTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "Indexing",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
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

			var running []int

			err := i.db.Select(ctx, &running, `SELECT COUNT(*) FROM market_indexing_tasks WHERE task_id IS NOT NULL AND complete IS FALSE;`)
			if err != nil {
				return false, xerrors.Errorf("getting running tasks: %w", err)
			}

			if len(running) > i.maxCurrentForCluster {
				log.Debugf("Not scheduling %s as maximum parallel per cluster limit %d reached", i.TypeDetails().Name, i.maxCurrentForCluster)
				return false, nil
			}

			var pendings []struct {
				SpID      int64                   `db:"sp_id"`
				Sector    int64                   `db:"sector_number"`
				PieceCid  string                  `db:"piece_cid"`
				Size      int64                   `db:"piece_size"`
				CreatedAt time.Time               `db:"created_at"`
				Offset    int64                   `db:"piece_offset"`
				Proof     abi.RegisteredPoStProof `db:"reg_seal_proof"`
			}

			err = i.db.Select(ctx, &running, `SELECT sp_id, sector_number, piece_cid, piece_size, created_at, piece_offset, reg_seal_proof  
												   FROM market_indexing_tasks WHERE task_id IS NULL 
													ORDER BY created_at ASC;`)
			if err != nil {
				return false, xerrors.Errorf("getting pending tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			pending := pendings[0]

			_, err = tx.Exec(`UPDATE market_indexing_tasks SET task_id = $1 WHERE 
                                           sp_id = $2 AND 
                                           sector_number = $3 AND 
                                           piece_cid = $4 AND 
                                           piece_size = $5 AND 
                                           piece_offset = $6 AND 
                                           reg_seal_proof = $7`, id, pending.SpID, pending.Sector,
				pending.PieceCid, pending.Size, pending.Offset, pending.Proof)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
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
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM market_indexing_tasks WHERE task_id = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err)
		return ""
	}
	return spid
}

var _ = harmonytask.Reg(&IndexingTask{})
var _ harmonytask.TaskInterface = &IndexingTask{}
