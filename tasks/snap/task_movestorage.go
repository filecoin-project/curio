package snap

import (
	"context"
	"github.com/filecoin-project/curio/lib/passcall"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
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

	_, err = m.db.Exec(ctx, `UPDATE sectors_snap_pipeline SET after_move_storage = TRUE, task_id_move_storage = NULL WHERE task_id_move_storage = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	return true, nil
}

func (m *MoveStorageTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (m *MoveStorageTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if seal.IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}
	return harmonytask.TaskTypeDetails{
		Max:  m.max,
		Name: "UpdateStore",
		Cost: resources.Resources{
			Cpu:     1,
			Ram:     512 << 20,
			Storage: m.sc.Storage(m.taskToSector, storiface.FTNone, storiface.FTUpdate|storiface.FTUpdateCache|storiface.FTUnsealed, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(MinSnapSchedInterval, func(taskFunc harmonytask.AddTaskFunc) error {
			return m.schedule(context.Background(), taskFunc)
		}),
	}
}

func (m *MoveStorageTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
	}

	err := m.db.Select(ctx, &tasks, `SELECT sp_id, sector_number FROM sectors_snap_pipeline WHERE data_assigned = true AND after_encode = true AND after_prove = TRUE AND after_move_storage = FALSE AND task_id_encode IS NULL`)
	if err != nil {
		return xerrors.Errorf("getting tasks: %w", err)
	}

	for _, t := range tasks {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			_, err := tx.Exec(`UPDATE sectors_snap_pipeline SET task_id_move_storage = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			return true, nil
		})
	}

	return nil
}

func (m *MoveStorageTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
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

var _ harmonytask.TaskInterface = &MoveStorageTask{}
