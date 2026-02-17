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
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/slotmgr"
	"github.com/filecoin-project/curio/lib/storiface"
)

// RSealProviderCleanup removes all sector data (sealed, cache, unsealed) from storage
// after the client has finished with the sector. This is triggered either by an explicit
// cleanup request from the client or by the cleanup timeout expiring.
type RSealProviderCleanup struct {
	db      *harmonydb.DB
	sp      *RSealProviderPoller
	storage *paths.Remote

	// Batch slot manager, may be nil if not using batch sealing
	slots *slotmgr.SlotMgr

	max int
}

func NewProviderCleanupTask(db *harmonydb.DB, sp *RSealProviderPoller, storage *paths.Remote, slots *slotmgr.SlotMgr, maxCleanup int) *RSealProviderCleanup {
	return &RSealProviderCleanup{
		db:      db,
		sp:      sp,
		storage: storage,
		slots:   slots,
		max:     maxCleanup,
	}
}

func (c *RSealProviderCleanup) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = c.db.Select(ctx, &sectors, `SELECT sp_id, sector_number, reg_seal_proof
		FROM rseal_provider_pipeline
		WHERE task_id_cleanup = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector for cleanup: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for cleanup, got %d", len(sectors))
	}
	task := sectors[0]

	sectorID := abi.SectorID{
		Miner:  abi.ActorID(task.SpID),
		Number: abi.SectorNumber(task.SectorNumber),
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Remove sealed data
	if err := c.storage.Remove(ctx, sectorID, storiface.FTSealed, true, nil); err != nil {
		log.Warnw("cleanup: failed to remove sealed data (may not exist)", "error", err,
			"sp", task.SpID, "sector", task.SectorNumber)
	}

	// Remove cache data
	if err := c.storage.Remove(ctx, sectorID, storiface.FTCache, true, nil); err != nil {
		log.Warnw("cleanup: failed to remove cache data (may not exist)", "error", err,
			"sp", task.SpID, "sector", task.SectorNumber)
	}

	// Remove unsealed data (if any)
	if err := c.storage.Remove(ctx, sectorID, storiface.FTUnsealed, true, nil); err != nil {
		log.Warnw("cleanup: failed to remove unsealed data (may not exist)", "error", err,
			"sp", task.SpID, "sector", task.SectorNumber)
	}

	// If this sector was part of a batch, release the batch slot
	if c.slots != nil {
		var batchRefs []struct {
			PipelineSlot int64  `db:"pipeline_slot"`
			HostAndPort  string `db:"machine_host_and_port"`
		}

		// Look up batch refs for this sector
		err = c.db.Select(ctx, &batchRefs, `SELECT pipeline_slot, machine_host_and_port
			FROM batch_sector_refs
			WHERE sp_id = $1 AND sector_number = $2`, task.SpID, task.SectorNumber)
		if err != nil {
			log.Warnw("cleanup: failed to query batch refs", "error", err,
				"sp", task.SpID, "sector", task.SectorNumber)
		}

		for _, ref := range batchRefs {
			if err := c.slots.SectorDone(ctx, uint64(ref.PipelineSlot), sectorID); err != nil {
				log.Warnw("cleanup: failed to release batch slot", "error", err,
					"sp", task.SpID, "sector", task.SectorNumber, "slot", ref.PipelineSlot)
			}
		}
	}

	// Mark cleanup as done
	n, err := c.db.Exec(ctx, `UPDATE rseal_provider_pipeline
		SET after_cleanup = TRUE, task_id_cleanup = NULL
		WHERE sp_id = $1 AND sector_number = $2 AND task_id_cleanup = $3`,
		task.SpID, task.SectorNumber, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating cleanup status: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row for cleanup, updated %d", n)
	}

	log.Infow("cleaned up remote seal sector",
		"sp", task.SpID,
		"sector", task.SectorNumber)

	return true, nil
}

func (c *RSealProviderCleanup) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// Cleanup can run on any node with storage access. Accept all.
	return ids, nil
}

func (c *RSealProviderCleanup) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(c.max),
		Name: "RSealProvCleanup",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 10,
		RetryWait:   taskhelp.RetryWaitLinear(60*time.Second, 30*time.Second),
	}
}

func (c *RSealProviderCleanup) Adder(taskFunc harmonytask.AddTaskFunc) {
	c.sp.pollers[pollerProvCleanup].Set(taskFunc)
}

func (c *RSealProviderCleanup) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := c.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (c *RSealProviderCleanup) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number
		FROM rseal_provider_pipeline
		WHERE task_id_cleanup = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, xerrors.Errorf("getting sector id for cleanup task: %w", err)
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealProviderCleanup{})
var _ harmonytask.TaskInterface = &RSealProviderCleanup{}
