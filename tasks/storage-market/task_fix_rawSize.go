package storage_market

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/market"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
)

type FixRawSize struct {
	db            *harmonydb.DB
	sc            *ffi.SealCalls
	pieceProvider *pieceprovider.SectorReader
}

func NewFixRawSize(db *harmonydb.DB, sc *ffi.SealCalls, pieceProvider *pieceprovider.SectorReader) *FixRawSize {
	return &FixRawSize{
		db:            db,
		sc:            sc,
		pieceProvider: pieceProvider,
	}
}

func (f *FixRawSize) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var id, pieceCidStr string
	var spID, sectorNumer, pieceOffset, pieceSize, proof int64
	err = f.db.QueryRow(ctx, `SELECT f.id, mpd.sp_id, mpd.sector_num, mpd.piece_cid, mpd.piece_offset, mpd.piece_length, m.reg_seal_proof 
									FROM market_fix_raw_size f 
									INNER JOIN market_piece_deal mpd ON f.id = mpd.id
									INNER JOIN sectors_meta m ON mpd.sp_id = m.sp_id AND mpd.sector_num = m.sector_num
									WHERE  f.task_id = $1 AND mpd.raw_size = 0 
									    AND piece_offset IS NOT NULL 
									LIMIT 1`, taskID).Scan(&id, &spID, &sectorNumer, &pieceCidStr, &pieceOffset, &pieceSize, &proof)
	if err != nil {
		return false, xerrors.Errorf("getting task from DB: %w", err)
	}

	pcid, err := cid.Parse(pieceCidStr)
	if err != nil {
		return false, xerrors.Errorf("parsing piece cid: %w", err)
	}

	reader, err := f.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(spID),
			Number: abi.SectorNumber(sectorNumer),
		},
		ProofType: abi.RegisteredSealProof(proof),
	}, storiface.PaddedByteIndex(pieceOffset).Unpadded(), abi.PaddedPieceSize(pieceSize).Unpadded(), pcid)

	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	defer func() {
		_ = reader.Close()
	}()

	carSize, err := market.GetRawSizeFromCarReader(reader)
	if err != nil {
		return false, xerrors.Errorf("getting raw size from CAR: %w", err)
	}

	if carSize == 0 {
		return false, xerrors.Errorf("car size is zero, something went wrong while reading the car")
	}

	if padreader.PaddedSize(carSize).Padded() != abi.PaddedPieceSize(pieceSize) {
		return false, xerrors.Errorf("car size does not match piece size")
	}

	comm, err := f.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE market_piece_deal SET raw_size = $1 WHERE id = $2 AND raw_size = 0`, carSize, id)
		if err != nil {
			return false, xerrors.Errorf("failed to update raw size in DB: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("failed to update raw size in DB: expected 1 rows affected, got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM market_fix_raw_size WHERE task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("deleting market_fix_raw_size: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("committing transaction: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("transaction didn't commit")
	}

	return true, nil
}

func (f *FixRawSize) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var acceptedIDs []harmonytask.TaskID

	err := f.db.QueryRow(ctx, `SELECT COALESCE(array_agg(task_id), '{}')::bigint[] AS task_ids FROM (
						SELECT f.task_id FROM market_fix_raw_size f
						INNER JOIN market_piece_deal mpd ON f.id = mpd.id
						INNER JOIN sector_location l ON mpd.sp_id = l.miner_id AND mpd.sector_num = l.sector_num AND l.sector_filetype = 1
						INNER JOIN storage_path sp ON sp.storage_id = l.storage_id 
						WHERE f.task_id = ANY($1::bigint[])  AND sp.urls IS NOT NULL AND sp.urls LIKE '%' || $2 || '%' LIMIT 100) s`, indIDs, engine.Host()).Scan(&acceptedIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks from DB: %w", err)
	}

	return acceptedIDs, nil
}

func (f *FixRawSize) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(16),
		Name: "FixRawSize",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Minute, func(taskFunc harmonytask.AddTaskFunc) error {
			return f.schedule(context.Background(), taskFunc)
		}),
	}
}

func (f *FixRawSize) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool

	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var running int64
			err := tx.QueryRow(`SELECT COUNT(*) FROM harmony_task WHERE name = $1`, "FixRawSize").Scan(&running)
			if err != nil {
				return false, xerrors.Errorf("getting running FixRawSize tasks: %w", err)
			}

			if running >= 100 {
				return false, nil
			}

			var tasks []struct {
				ID string `db:"id"`
			}

			err = tx.Select(&tasks, `SELECT mpd.id FROM market_piece_deal mpd
          									WHERE mpd.raw_size = 0 
          									  AND mpd.piece_offset IS NOT NULL 
          									  AND NOT EXISTS(SELECT 1 FROM market_fix_raw_size f WHERE f.id = mpd.id) 
          									      LIMIT 1`)
			if err != nil {
				return false, xerrors.Errorf("getting rows with 0 raw size: %w", err)
			}

			if len(tasks) == 0 {
				return false, nil
			}

			n, err := tx.Exec(`INSERT INTO market_fix_raw_size (id, task_id) VALUES ($1, $2)`, tasks[0].ID, id)
			if err != nil {
				return false, xerrors.Errorf("scheduling market_fix_raw_size: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("scheduling market_fix_raw_size: %d rows affected", n)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})
	}

	return nil
}

func (f *FixRawSize) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ = harmonytask.Reg(&FixRawSize{})
var _ harmonytask.TaskInterface = &FixRawSize{}
