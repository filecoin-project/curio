package remoteseal

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/market/sealmarket"
)

// RSealDelegate intercepts sectors before normal SDR processing and delegates
// them to remote providers. Uses the IAmBored pattern like SupraSeal's schedule().
//
// The schedule() callback only does fast DB operations to claim sectors.
// The expensive HTTP dance (CheckAvailable + SendOrder) happens in Do() so
// the scheduling loop is not blocked.
type RSealDelegate struct {
	db     *harmonydb.DB
	client *RSealClient
}

func NewRSealDelegate(db *harmonydb.DB, client *RSealClient) *RSealDelegate {
	return &RSealDelegate{
		db:     db,
		client: client,
	}
}

type availableProvider struct {
	ID    int64  `db:"id"`
	URL   string `db:"provider_url"`
	Token string `db:"provider_token"`
}

type candidateSector struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`
	RegSealProof int   `db:"reg_seal_proof"`
}

// schedule is the IAmBored callback. It finds unclaimed sectors that have enabled
// providers and atomically claims them in the DB. No HTTP calls happen here —
// the expensive provider interaction is deferred to Do().
func (d *RSealDelegate) schedule(taskFunc harmonytask.AddTaskFunc) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find sectors ready for SDR that are not yet claimed by any task and
	// have no existing rseal_client_pipeline entry, but DO have an enabled provider.
	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		RegSealProof int   `db:"reg_seal_proof"`
		ProviderID   int64 `db:"provider_id"`
	}
	err := d.db.Select(ctx, &sectors, `
		SELECT s.sp_id, s.sector_number, s.reg_seal_proof, p.id AS provider_id
		FROM sectors_sdr_pipeline s
		JOIN rseal_client_providers p ON p.sp_id = s.sp_id AND p.enabled = TRUE
		WHERE s.after_sdr = FALSE
		  AND s.task_id_sdr IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM rseal_client_pipeline c
			WHERE c.sp_id = s.sp_id
			  AND c.sector_number = s.sector_number
		  )
		LIMIT 1`)
	if err != nil {
		return xerrors.Errorf("finding candidate sectors: %w", err)
	}

	if len(sectors) == 0 {
		return nil
	}

	sector := sectors[0]

	// Atomically claim the sector in the DB. Do() will handle the HTTP calls.
	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		// Insert into rseal_client_pipeline
		n, err := tx.Exec(`
			INSERT INTO rseal_client_pipeline (sp_id, sector_number, provider_id, reg_seal_proof)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (sp_id, sector_number) DO NOTHING`,
			sector.SpID, sector.SectorNumber, sector.ProviderID, sector.RegSealProof)
		if err != nil {
			return false, xerrors.Errorf("inserting rseal_client_pipeline: %w", err)
		}
		if n == 0 {
			return false, nil // already claimed
		}

		// Claim the sector in sectors_sdr_pipeline by setting all SDR/tree task_ids
		// to this task's ID. This prevents the local SDR poller from assigning tasks.
		n, err = tx.Exec(`
			UPDATE sectors_sdr_pipeline
			SET task_id_sdr = $1, task_id_tree_d = $1, task_id_tree_c = $1, task_id_tree_r = $1
			WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL`,
			id, sector.SpID, sector.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("claiming sector in sdr_pipeline: %w", err)
		}
		if n != 1 {
			return false, nil // someone else claimed it
		}

		return true, nil
	})

	return nil
}

func (d *RSealDelegate) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Read the claimed sector and provider info
	var sectors []struct {
		SpID          int64  `db:"sp_id"`
		SectorNumber  int64  `db:"sector_number"`
		RegSealProof  int    `db:"reg_seal_proof"`
		ProviderURL   string `db:"provider_url"`
		ProviderToken string `db:"provider_token"`
	}

	err = d.db.Select(ctx, &sectors, `
		SELECT c.sp_id, c.sector_number, c.reg_seal_proof,
		       p.provider_url, p.provider_token
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers p ON c.provider_id = p.id
		WHERE c.task_id_sdr = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("querying sector for delegate task: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for delegate task, got %d", len(sectors))
	}
	sector := sectors[0]

	// Check provider availability
	availCtx, availCancel := context.WithTimeout(ctx, 10*time.Second)
	availResp, err := d.client.CheckAvailable(availCtx, sector.ProviderURL, sector.ProviderToken)
	availCancel()
	if err != nil {
		return false, xerrors.Errorf("checking provider availability: %w", err)
	}

	if !availResp.Available {
		// Provider not available right now — retry later
		return false, xerrors.Errorf("provider %s not available", sector.ProviderURL)
	}

	// Send order to provider
	orderResp, err := d.client.SendOrder(ctx, sector.ProviderURL, sector.ProviderToken, &sealmarket.OrderRequest{
		SlotToken:    availResp.SlotToken,
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
		RegSealProof: sector.RegSealProof,
	})
	if err != nil {
		return false, xerrors.Errorf("sending order to provider: %w", err)
	}

	if !orderResp.Accepted {
		// Provider rejected the order — fail permanently so the sector can be
		// re-assigned (the poller will clear task_id_sdr on failure)
		log.Warnw("provider rejected order",
			"provider", sector.ProviderURL,
			"reason", orderResp.RejectReason,
			"sp_id", sector.SpID,
			"sector", sector.SectorNumber)
		return false, xerrors.Errorf("provider rejected order: %s", orderResp.RejectReason)
	}

	log.Infow("delegated sector to remote provider",
		"sp_id", sector.SpID,
		"sector", sector.SectorNumber,
		"provider", sector.ProviderURL)

	// Order accepted. Task completes — the RSealClientPoll task will take over
	// to monitor progress. The task_id_sdr in rseal_client_pipeline will become
	// stale when harmonytask deletes this task entry, allowing the poller to
	// create poll tasks.
	return true, nil
}

func (d *RSealDelegate) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (d *RSealDelegate) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RSealDelegate",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 16 << 20, // 16 MiB - minimal, just HTTP calls
		},
		MaxFailures: 100,
		IAmBored:    passcall.Every(15*time.Second, d.schedule),
	}
}

func (d *RSealDelegate) Adder(taskFunc harmonytask.AddTaskFunc) {
	// IAmBored tasks don't use the Adder pattern
}

func (d *RSealDelegate) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := d.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (d *RSealDelegate) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(),
		`SELECT sp_id, sector_number FROM rseal_client_pipeline WHERE task_id_sdr = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealDelegate{})
var _ harmonytask.TaskInterface = &RSealDelegate{}
