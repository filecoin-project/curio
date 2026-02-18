package seal

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
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
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

func (m *MoveStorageTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

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
			return false, fmt.Errorf("error creating indexing task for mk12: %w", err)
		}

		_, err = tx.Exec(`UPDATE sectors_sdr_pipeline SET after_move_storage = TRUE, task_id_move_storage = NULL WHERE task_id_move_storage = $1`, taskID)
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
		err := stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(MinerTag, maddr.String()),
		}, SealMeasures.MoveStorageCompleted.M(1))
		if err != nil {
			log.Errorf("recording metric: %s", err)
		}
	}

	return true, nil
}

func (m *MoveStorageTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {

	ctx := context.Background()
	/*

			var tasks []struct {
				TaskID       harmonytask.TaskID `db:"task_id_finalize"`
				SpID         int64              `db:"sp_id"`
				SectorNumber int64              `db:"sector_number"`
				StorageID    string             `db:"storage_id"`
			}

			indIDs := make([]int64, len(ids))
			for i, id := range ids {
				indIDs[i] = int64(id)
			}
			err := m.db.Select(ctx, &tasks, `
				select p.task_id_move_storage, p.sp_id, p.sector_number, l.storage_id from sectors_sdr_pipeline p
				    inner join sector_location l on p.sp_id=l.miner_id and p.sector_number=l.sector_num
				    where task_id_move_storage in ($1) and l.sector_filetype=4`, indIDs)
			if err != nil {
				return []harmonytask.TaskID{}, xerrors.Errorf("getting tasks: %w", err)
			}

			ls, err := m.sc.LocalStorage(ctx)
			if err != nil {
				return []harmonytask.TaskID{}, xerrors.Errorf("getting local storage: %w", err)
			}

			acceptables := map[harmonytask.TaskID]bool{}

			for _, t := range ids {
				acceptables[t] = true
			}

			for _, t := range tasks {

			}

			todo some smarts
		     * yield a schedule cycle/s if we have moves already in progress
	*/

	////
	ls, err := m.sc.LocalStorage(ctx)
	if err != nil {
		return []harmonytask.TaskID{}, xerrors.Errorf("getting local storage: %w", err)
	}
	var haveStorage bool
	for _, l := range ls {
		if l.CanStore {
			haveStorage = true
			break
		}
	}

	if !haveStorage {
		return []harmonytask.TaskID{}, nil
	}

	return ids, nil
}

func (m *MoveStorageTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	cpu := 1
	if m.max > 0 {
		cpu = 0
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(m.max),
		Name: "MoveStorage",
		Cost: resources.Resources{
			Cpu:     cpu,
			Gpu:     0,
			Ram:     128 << 20,
			Storage: m.sc.Storage(m.taskToSector, storiface.FTNone, storiface.FTCache|storiface.FTSealed|storiface.FTUnsealed, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 6,
		RetryWait: func(retries int) time.Duration {
			return time.Duration(2<<retries) * time.Second
		},
	}
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
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_move_storage = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&MoveStorageTask{})

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
