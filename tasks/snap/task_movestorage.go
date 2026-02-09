package snap

import (
	"context"
	"fmt"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/seal"
)

type MoveStorageTask struct {
	max int

	sc *ffi.SealCalls
	db *harmonydb.DB
}

func NewMoveStorageTask(sc *ffi.SealCalls, db *harmonydb.DB, max int) *MoveStorageTask {
	return &MoveStorageTask{
		max: max,
		sc:  sc,
		db:  db,
	}
}

func (m *MoveStorageTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectorParamsArr []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = m.db.Select(ctx, &sectorParamsArr, `SELECT snp.sp_id, snp.sector_number, sm.reg_seal_proof
		FROM sectors_snap_pipeline snp INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_move_storage = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectorParamsArr) != 1 {
		return false, xerrors.Errorf("expected 1 sector, got %d", len(sectorParamsArr))
	}

	task := sectorParamsArr[0]

	sector := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: abi.SectorNumber(task.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(task.RegSealProof),
	}

	err = m.sc.MoveStorageSnap(ctx, sector, &taskID)
	if err != nil {
		return false, xerrors.Errorf("moving storage: %w", err)
	}

	_, err = m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Set indexing_created_at to Now() to allow new indexing tasks
		_, err = tx.Exec(`UPDATE market_mk20_pipeline
							SET indexing_created_at = NOW()
							WHERE sp_id = $1 AND sector = $2;`, task.SpID, task.SectorNumber)
		if err != nil {
			return false, fmt.Errorf("error creating indexing task for mk20 deals: %w", err)
		}

		_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline
							SET indexing_created_at = NOW()
							WHERE sp_id = $1 AND sector = $2;
						`, task.SpID, task.SectorNumber)
		if err != nil {
			return false, fmt.Errorf("error creating indexing task for mk12 deals: %w", err)
		}

		_, err = tx.Exec(`UPDATE sectors_snap_pipeline SET after_move_storage = TRUE, task_id_move_storage = NULL WHERE task_id_move_storage = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("updating task: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("committing transaction: %w", err)
	}

	// Record metric
	if maddr, err := address.NewIDAddress(uint64(task.SpID)); err == nil {
		if err := stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(MinerTag, maddr.String()),
		}, SnapMeasures.MoveStorageCompleted.M(1)); err != nil {
			log.Errorf("recording metric: %s", err)
		}
	}

	return true, nil
}

func (m *MoveStorageTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (m *MoveStorageTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if seal.IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	cpu := 1
	if m.max > 0 {
		cpu = 0
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(m.max),
		Name: "UpdateStore",
		Cost: resources.Resources{
			Cpu:     cpu,
			Ram:     128 << 20,
			Storage: m.sc.Storage(m.taskToSector, storiface.FTNone, storiface.FTUpdate|storiface.FTUpdateCache|storiface.FTUnsealed, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 6,
		RetryWait: func(retries int) time.Duration {
			return time.Duration(2<<retries) * time.Second
		},
		IAmBored: passcall.Every(MinSnapSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return m.schedule(context.Background(), taskFunc)
		}),
	}
}

func (m *MoveStorageTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var tasks []struct {
				SpID         int64 `db:"sp_id"`
				SectorNumber int64 `db:"sector_number"`
			}

			err := tx.Select(&tasks, `SELECT sp_id, sector_number FROM sectors_snap_pipeline WHERE after_encode = TRUE AND after_move_storage = FALSE AND task_id_move_storage IS NULL ORDER BY start_time ASC LIMIT 1`)
			if err != nil {
				return false, xerrors.Errorf("getting tasks: %w", err)
			}

			if len(tasks) == 0 {
				return false, nil
			}

			t := tasks[0]

			_, err = tx.Exec(`UPDATE sectors_snap_pipeline SET task_id_move_storage = $1 WHERE sp_id = $2 AND sector_number = $3 AND after_encode = TRUE AND after_move_storage = FALSE AND task_id_move_storage IS NULL`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (m *MoveStorageTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (m *MoveStorageTask) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := m.db.Select(context.Background(), &refs, `SELECT snp.sp_id, snp.sector_number, sm.reg_seal_proof
		FROM sectors_snap_pipeline snp INNER JOIN sectors_meta sm ON snp.sp_id = sm.sp_id AND snp.sector_number = sm.sector_num
		WHERE snp.task_id_move_storage = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (m *MoveStorageTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := m.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (m *MoveStorageTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_snap_pipeline WHERE task_id_move_storage = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&MoveStorageTask{})
var _ harmonytask.TaskInterface = &MoveStorageTask{}
