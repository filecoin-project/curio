package seal

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/slotmgr"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type FinalizeTask struct {
	max int
	sp  *SealPoller
	sc  *ffi.SealCalls
	db  *harmonydb.DB

	// Batch, nillable!
	slots *slotmgr.SlotMgr
}

func NewFinalizeTask(max int, sp *SealPoller, sc *ffi.SealCalls, db *harmonydb.DB, slots *slotmgr.SlotMgr) *FinalizeTask {
	return &FinalizeTask{
		max: max,
		sp:  sp,
		sc:  sc,
		db:  db,

		slots: slots,
	}
}

func (f *FinalizeTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	ctx := context.Background()

	err = f.db.Select(ctx, &tasks, `
		SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_finalize = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting task: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected one task")
	}
	task := tasks[0]

	var keepUnsealed bool

	if err := f.db.QueryRow(ctx, `SELECT COALESCE(BOOL_OR(NOT data_delete_on_finalize), FALSE) FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, task.SpID, task.SectorNumber).Scan(&keepUnsealed); err != nil {
		return false, err
	}

	sector := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: abi.SectorNumber(task.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(task.RegSealProof),
	}

	var ownedBy []struct {
		HostAndPort string `db:"machine_host_and_port"`
	}
	var refs []struct {
		PipelineSlot int64 `db:"pipeline_slot"`
	}

	if f.slots != nil {
		// batch handling part 1:
		// get machine id

		err = f.db.Select(ctx, &ownedBy, `SELECT hm.host_and_port FROM harmony_task INNER JOIN harmony_machines hm on harmony_task.owner_id = hm.id WHERE harmony_task.id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("getting machine id: %w", err)
		}

		if len(ownedBy) != 1 {
			return false, xerrors.Errorf("expected one machine")
		}

		/*
			CREATE TABLE batch_sector_refs (
			    sp_id BIGINT NOT NULL,
			    sector_number BIGINT NOT NULL,

			    machine_host_and_port TEXT NOT NULL,
			    pipeline_slot BIGINT NOT NULL,

			    PRIMARY KEY (sp_id, sector_number, machine_host_and_port, pipeline_slot),
			    FOREIGN KEY (sp_id, sector_number) REFERENCES sectors_sdr_pipeline (sp_id, sector_number)
			);
		*/

		// select the ref by sp_id and sector_number
		// if we (ownedBy) are NOT the same as machine_host_and_port, then fail this finalize, it's really bad, and not our job

		err = f.db.Select(ctx, &refs, `SELECT pipeline_slot FROM batch_sector_refs WHERE sp_id = $1 AND sector_number = $2 AND machine_host_and_port = $3`, task.SpID, task.SectorNumber, ownedBy[0].HostAndPort)
		if err != nil {
			return false, xerrors.Errorf("getting batch refs: %w", err)
		}

		if len(refs) != 1 {
			return false, xerrors.Errorf("expected one batch ref")
		}
	}

	err = f.sc.FinalizeSector(ctx, sector, keepUnsealed)
	if err != nil {
		return false, xerrors.Errorf("finalizing sector: %w", err)
	}

	if err := DropSectorPieceRefs(ctx, f.db, sector.ID); err != nil {
		return false, xerrors.Errorf("dropping sector piece refs: %w", err)
	}

	if f.slots != nil {
		// batch handling part 2:

		// delete from batch_sector_refs
		var freeSlot bool

		_, err = f.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			_, err = tx.Exec(`DELETE FROM batch_sector_refs WHERE sp_id = $1 AND sector_number = $2`, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("deleting batch refs: %w", err)
			}

			// get sector ref count, if zero free the pipeline slot
			var count []struct {
				Count int64 `db:"count"`
			}
			err = tx.Select(&count, `SELECT COUNT(1) as count FROM batch_sector_refs WHERE machine_host_and_port = $1 AND pipeline_slot = $2`, ownedBy[0].HostAndPort, refs[0].PipelineSlot)
			if err != nil {
				return false, xerrors.Errorf("getting batch ref count: %w", err)
			}

			if count[0].Count == 0 {
				freeSlot = true
			} else {
				log.Infow("Not freeing batch slot", "slot", refs[0].PipelineSlot, "machine", ownedBy[0].HostAndPort, "remaining", count[0].Count)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, xerrors.Errorf("deleting batch refs: %w", err)
		}

		if freeSlot {
			log.Infow("Freeing batch slot", "slot", refs[0].PipelineSlot, "machine", ownedBy[0].HostAndPort)
			if err := f.slots.Put(uint64(refs[0].PipelineSlot)); err != nil {
				return false, xerrors.Errorf("freeing slot: %w", err)
			}
		}
	}

	// set after_finalize
	_, err = f.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_finalize = TRUE, task_id_finalize = NULL WHERE task_id_finalize = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	return true, nil
}

func (f *FinalizeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"task_id_finalize"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector_number"`
		StorageID    string             `db:"storage_id"`
	}

	if storiface.FTCache != 4 {
		panic("storiface.FTCache != 4")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	err := f.db.Select(ctx, &tasks, `
		SELECT p.task_id_finalize, p.sp_id, p.sector_number, l.storage_id FROM sectors_sdr_pipeline p
			INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num
			WHERE task_id_finalize = ANY ($1) AND l.sector_filetype = 4
`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks: %w", err)
	}

	ls, err := f.sc.LocalStorage(ctx)
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

func (f *FinalizeTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  f.max,
		Name: "Finalize",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 100 << 20,
		},
		MaxFailures: 10,
	}
}

func (f *FinalizeTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.sp.pollers[pollerFinalize].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &FinalizeTask{}
