package remoteseal

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/storiface"
)

// RSealProviderFinalize drops SDR layers (cache) after C1 has been supplied.
// This is similar to the regular FinalizeTask but operates on rseal_provider_pipeline
// and does not need to handle unsealed data or deal pieces (delegated sectors are CC).
type RSealProviderFinalize struct {
	db *harmonydb.DB
	sp *RSealProviderPoller
	sc *ffi.SealCalls

	max int
}

func NewProviderFinalizeTask(db *harmonydb.DB, sp *RSealProviderPoller, sc *ffi.SealCalls, maxFinalize int) *RSealProviderFinalize {
	return &RSealProviderFinalize{
		db:  db,
		sp:  sp,
		sc:  sc,
		max: maxFinalize,
	}
}

func (f *RSealProviderFinalize) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = f.db.Select(ctx, &sectors, `SELECT sp_id, sector_number, reg_seal_proof
		FROM rseal_provider_pipeline
		WHERE task_id_finalize = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector for finalize: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for finalize, got %d", len(sectors))
	}
	task := sectors[0]

	sector := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SpID),
			Number: abi.SectorNumber(task.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(task.RegSealProof),
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// For remote seal finalize, we clear the cache (drop SDR layers).
	// Delegated sectors are always CC (no unsealed data to preserve).
	err = f.sc.FinalizeSector(ctx, sector, false)
	if err != nil {
		return false, xerrors.Errorf("finalizing remote seal sector: %w", err)
	}

	// Mark finalize as done
	n, err := f.db.Exec(ctx, `UPDATE rseal_provider_pipeline
		SET after_finalize = TRUE, task_id_finalize = NULL
		WHERE sp_id = $1 AND sector_number = $2 AND task_id_finalize = $3`,
		task.SpID, task.SectorNumber, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating finalize status: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row for finalize, updated %d", n)
	}

	log.Infow("finalized remote seal sector",
		"sp", task.SpID,
		"sector", task.SectorNumber)

	return true, nil
}

func (f *RSealProviderFinalize) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// Check that the sector's cache is on local storage, similar to the regular finalize task.
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
		SELECT p.task_id_finalize, p.sp_id, p.sector_number, l.storage_id
		FROM rseal_provider_pipeline p
			INNER JOIN sector_location l ON p.sp_id = l.miner_id AND p.sector_number = l.sector_num
		WHERE task_id_finalize = ANY ($1) AND l.sector_filetype = 4`, indIDs)
	if err != nil {
		return []harmonytask.TaskID{}, xerrors.Errorf("getting finalize tasks: %w", err)
	}

	ls, err := f.sc.LocalStorage(ctx)
	if err != nil {
		return []harmonytask.TaskID{}, xerrors.Errorf("getting local storage: %w", err)
	}

	acceptables := map[harmonytask.TaskID]bool{}
	for _, id := range ids {
		acceptables[id] = true
	}

	var result []harmonytask.TaskID
	for _, t := range tasks {
		if _, ok := acceptables[t.TaskID]; !ok {
			continue
		}

		for _, l := range ls {
			if string(l.ID) == t.StorageID {
				result = append(result, t.TaskID)
			}
		}
	}

	return result, nil
}

func (f *RSealProviderFinalize) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(f.max),
		Name: "RSealProvFinal",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 100 << 20,
		},
		MaxFailures: 10,
		RetryWait:   taskhelp.RetryWaitLinear(30*time.Second, 15*time.Second),
	}
}

func (f *RSealProviderFinalize) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.sp.pollers[pollerProvFinalize].Set(taskFunc)
}

func (f *RSealProviderFinalize) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := f.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (f *RSealProviderFinalize) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number
		FROM rseal_provider_pipeline
		WHERE task_id_finalize = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, xerrors.Errorf("getting sector id for finalize task: %w", err)
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealProviderFinalize{})
var _ harmonytask.TaskInterface = &RSealProviderFinalize{}
