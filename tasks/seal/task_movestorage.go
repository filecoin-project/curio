package seal

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	storiface "github.com/filecoin-project/curio/lib/storiface"
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

	// Create a indexing task
	var pms []struct {
		Id      string `db:"uuid"`
		SpID    int64  `db:"sp_id"`
		Sector  int64  `db:"sector_number"`
		Proof   int64  `db:"reg_seal_proof"`
		Pcid    string `db:"piece_cid"`
		Psize   int64  `db:"piece_size"`
		Poffset int64  `db:"piece_offset"`
	}

	err = m.db.Select(ctx, &pms, `SELECT
    							dp.uuid,
								ssp.sp_id, 
								ssp.sector_number, 
								ssp.reg_seal_proof, 
								smp.piece_cid, 
								dp.sector_offset as piece_offset, 
								smp.piece_size
							FROM 
								sectors_sdr_pipeline ssp
							JOIN 
								sectors_meta_pieces smp ON ssp.sp_id = smp.sp_id AND ssp.sector_number = smp.sector_num
							JOIN 
								market_mk12_deal_pipeline dp ON smp.piece_cid = dp.piece_cid AND smp.sp_id = dp.sp_id AND smp.sector_num = dp.sector
							WHERE 
								ssp.task_id_move_storage = $1;
							`, taskID)

	if err != nil {
		return false, xerrors.Errorf("getting piece details to create indexing task: %w", err)
	}

	// A sector can have 0 pieces, more than 1 piece or even same piece twice from different deals
	if len(pms) > 0 {
		comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			for _, pm := range pms {
				n, err := tx.Exec(`INSERT INTO market_indexing_tasks (id, sp_id, sector_number, reg_seal_proof, piece_cid, piece_offset, piece_size) 
									values ($1, $2, $3, $4, $5, $6, $7)`, pm.Id, pm.SpID, pm.Sector, pm.Proof, pm.Pcid, pm.Poffset, pm.Psize)
				if err != nil {
					return false, xerrors.Errorf("inserting an indexing task for miner %d sector %d deal %s", pm.SpID, pm.Sector, pm.Id)
				}
				if n != 1 {
					return false, xerrors.Errorf("expected 1 row but got %d", n)
				}
				_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET sealed = TRUE WHERE uuid = $1`, pm.Id)
				if err != nil {
					return false, xerrors.Errorf("failed to mark deal %s as complete", pm.Id)
				}
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, err
		}
		if !comm {
			return false, xerrors.Errorf("failed to commit the transaction")
		}
	}

	return true, nil
}

func (m *MoveStorageTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {

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
				return nil, xerrors.Errorf("getting tasks: %w", err)
			}

			ls, err := m.sc.LocalStorage(ctx)
			if err != nil {
				return nil, xerrors.Errorf("getting local storage: %w", err)
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
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}
	var haveStorage bool
	for _, l := range ls {
		if l.CanStore {
			haveStorage = true
			break
		}
	}

	if !haveStorage {
		return nil, nil
	}

	id := ids[0]
	return &id, nil
}

func (m *MoveStorageTask) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if IsDevnet {
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
