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
	"github.com/filecoin-project/curio/market/sealmarket"
)

// RSealClientCleanup tells the remote provider to clean up after PoRep is done.
// First sends a finalize request (drop layers), then sends a cleanup request
// (full data removal).
type RSealClientCleanup struct {
	db     *harmonydb.DB
	client *RSealClient
	sp     *RSealClientPoller
}

func NewRSealClientCleanup(db *harmonydb.DB, client *RSealClient, sp *RSealClientPoller) *RSealClientCleanup {
	return &RSealClientCleanup{
		db:     db,
		client: client,
		sp:     sp,
	}
}

func (t *RSealClientCleanup) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Find the sector assigned to this cleanup task
	var sectors []struct {
		SpID          int64  `db:"sp_id"`
		SectorNumber  int64  `db:"sector_number"`
		ProviderURL   string `db:"provider_url"`
		ProviderToken string `db:"provider_token"`
	}

	err = t.db.Select(ctx, &sectors, `
		SELECT c.sp_id, c.sector_number, pr.provider_url, pr.provider_token
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers pr ON c.provider_id = pr.id
		WHERE c.task_id_cleanup = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("querying sector for cleanup: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for cleanup, got %d", len(sectors))
	}
	sector := sectors[0]

	// Send finalize to provider (drop layers, keep sealed+cache for potential C1 retries)
	err = t.client.SendFinalize(ctx, sector.ProviderURL, sector.ProviderToken, &sealmarket.FinalizeRequest{
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
	})
	if err != nil {
		return false, xerrors.Errorf("sending finalize to provider: %w", err)
	}

	// Send cleanup to provider (full data removal)
	err = t.client.SendCleanup(ctx, sector.ProviderURL, sector.ProviderToken, &sealmarket.CleanupRequest{
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
	})
	if err != nil {
		return false, xerrors.Errorf("sending cleanup to provider: %w", err)
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Mark cleanup as done
	_, err = t.db.Exec(ctx, `
		UPDATE rseal_client_pipeline
		SET after_cleanup = TRUE, task_id_cleanup = NULL
		WHERE sp_id = $1 AND sector_number = $2`,
		sector.SpID, sector.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("updating rseal_client_pipeline: %w", err)
	}

	log.Infow("remote seal cleanup completed",
		"sp_id", sector.SpID, "sector", sector.SectorNumber)

	return true, nil
}

func (t *RSealClientCleanup) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *RSealClientCleanup) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RSealCleanup",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 16 << 20, // 16 MiB - just HTTP calls
		},
		MaxFailures: 50,
		RetryWait:   taskhelp.RetryWaitLinear(60*time.Second, 60*time.Second),
	}
}

func (t *RSealClientCleanup) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.sp.pollers[pollerClientCleanup].Set(taskFunc)
}

func (t *RSealClientCleanup) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *RSealClientCleanup) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(),
		`SELECT sp_id, sector_number FROM rseal_client_pipeline WHERE task_id_cleanup = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealClientCleanup{})
var _ harmonytask.TaskInterface = &RSealClientCleanup{}
