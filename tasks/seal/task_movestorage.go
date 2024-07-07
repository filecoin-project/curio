package seal

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/samber/lo"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type MoveStorageTask struct {
	sp *SealPoller
	sc *ffi2.SealCalls
	db *harmonydb.DB

	max int
}

func NewMoveStorageTask(sp *SealPoller, sc *ffi2.SealCalls, db *harmonydb.DB, max int) *MoveStorageTask {
	return &MoveStorageTask{
		max: max,
		sp:  sp,
		sc:  sc,
		db:  db,
	}
}

func (m *MoveStorageTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	ctx := context.Background()

	err = m.db.Select(ctx, &tasks, `
		SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_move_storage = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting task: %w", err)
	}
	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected one task")
	}
	task := tasks[0]

	sector := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: abi.SectorNumber(task.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(task.RegSealProof),
	}

	err = m.sc.MoveStorage(ctx, sector, &taskID)
	if err != nil {
		return false, xerrors.Errorf("moving storage: %w", err)
	}

	_, err = m.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_move_storage = TRUE, task_id_move_storage = NULL WHERE task_id_move_storage = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	return true, nil
}

func (m *MoveStorageTask) CanAccept(ids []harmonytask.TaskID, schedInfo *harmonytask.SchedulingInfo) (*harmonytask.TaskID, error) {
	ctx := context.Background()

	ls, err := m.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}
	if _, ok := lo.Find(ls, func(l storiface.StoragePath) bool { return l.CanStore }); !ok {
		return nil, nil
	}
	// TODO apply filter logic to ensure this storage is valid for the thing we're moving

	id := ids[0]
	return &id, nil
}

func (m *MoveStorageTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if isDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	return harmonytask.TaskTypeDetails{
		Max:  m.max,
		Name: "MoveStorage",
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     0,
			Ram:     128 << 20,
			Storage: m.sc.Storage(m.taskToSector, storiface.FTNone, storiface.FTCache|storiface.FTSealed|storiface.FTUnsealed, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 10,
	}
}

func (m *MoveStorageTask) GetSpid(taskID int64) string {
	var spid string
	err := m.db.QueryRow(context.Background(), `SELECT sp_id FROM sectors_sdr_pipeline WHERE task_id_move_storage = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err)
		return ""
	}
	return spid
}

func (m *MoveStorageTask) taskToSector(id harmonytask.TaskID) (ffi2.SectorRef, error) {
	var refs []ffi2.SectorRef

	err := m.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_move_storage = $1`, id)
	if err != nil {
		return ffi2.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi2.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (m *MoveStorageTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	m.sp.pollers[pollerMoveStorage].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &MoveStorageTask{}
