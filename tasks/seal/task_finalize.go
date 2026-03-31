package seal

import (
	"context"
	"fmt"

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
	"github.com/filecoin-project/curio/lib/slotmgr"
	"github.com/filecoin-project/curio/lib/storiface"
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

func (f *FinalizeTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := f.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (f *FinalizeTask) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_finalize = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&FinalizeTask{})

func (f *FinalizeTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

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
		HostAndPort string `db:"host_and_port"`
	}
	var refs []struct {
		PipelineSlot int64 `db:"pipeline_slot"`
	}

	var refFound bool
	if f.slots != nil {
		// batch handling part 1:
		// get machine id

		err = f.db.Select(ctx, &ownedBy, `SELECT hm.host_and_port as host_and_port FROM harmony_task INNER JOIN harmony_machines hm on harmony_task.owner_id = hm.id WHERE harmony_task.id = $1`, taskID)
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

		if len(refs) > 1 {
			return false, xerrors.Errorf("expected one batch ref, got %d", len(refs))
		}

		refFound = len(refs) == 1
	}

	err = f.sc.FinalizeSector(ctx, sector, keepUnsealed)
	if err != nil {
		return false, xerrors.Errorf("finalizing sector: %w", err)
	}

	if err := DropSectorPieceRefs(ctx, f.db, sector.ID); err != nil {
		return false, xerrors.Errorf("dropping sector piece refs: %w", err)
	}

	if refFound {
		// batch handling part 2:

		if err := f.slots.SectorDone(ctx, uint64(refs[0].PipelineSlot), sector.ID); err != nil {
			return false, xerrors.Errorf("mark batch ref done: %w", err)
		}
	}

	// set after_finalize
	_, err = f.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_finalize = TRUE, task_id_finalize = NULL WHERE task_id_finalize = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating task: %w", err)
	}

	// Record metric
	if maddr, err := address.NewIDAddress(uint64(task.SpID)); err == nil {
		err := stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(MinerTag, maddr.String()),
		}, SealMeasures.FinalizeCompleted.M(1))
		if err != nil {
			log.Errorf("recording metric: %s", err)
		}
	}

	return true, nil
}

func (f *FinalizeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if storiface.FTCache != 4 {
		panic("storiface.FTCache != 4")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var acceptedIDs []harmonytask.TaskID
	err := f.db.QueryRow(ctx, `SELECT COALESCE(array_agg(task_id_finalize), '{}')::bigint[] AS task_ids_finalize FROM 
										  (
										      SELECT p.task_id_finalize FROM sectors_sdr_pipeline p
												INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num AND l.sector_filetype = 4
												INNER JOIN storage_path sp ON sp.storage_id = l.storage_id
												WHERE task_id_finalize = ANY ($1) 
												  AND sp.urls IS NOT NULL 
												  AND sp.urls LIKE '%' || $2 || '%' 
												  LIMIT 100
										  ) s`, indIDs, engine.Host()).Scan(&acceptedIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks from DB: %w", err)
	}

	return acceptedIDs, nil
}

func (f *FinalizeTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(f.max),
		Name: "Finalize",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 100 << 20,
		},
		MaxFailures: 10,

		// Allow finalize to be scheduled when batch tasks are still running even when the node is not schedulable.
		// This allows finalize to unblock move-storage and PoRep for multiple hours while the node is technically not schedulable,
		// but is still finishing another batch. In most cases this behavior enables nearly zero-waste restarts of supraseal nodes.
		SchedulingOverrides: batchTaskNameGrid(),
	}
}

func batchTaskNameGrid() map[string]bool {
	batchSizes := []int{128, 64, 32, 16, 8}
	sectorSizes := []string{"32G", "64G"}

	out := map[string]bool{}
	for _, batchSize := range batchSizes {
		for _, sectorSize := range sectorSizes {
			out[fmt.Sprintf("Batch%d-%s", batchSize, sectorSize)] = true
		}
	}
	return out
}

func (f *FinalizeTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.sp.pollers[pollerFinalize].Set(taskFunc)
}

var _ harmonytask.TaskInterface = &FinalizeTask{}
