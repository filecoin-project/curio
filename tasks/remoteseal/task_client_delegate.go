package remoteseal

import (
	"context"
	"sync"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/market/sealmarket"
	"github.com/filecoin-project/curio/tasks/seal"

	lotusapi "github.com/filecoin-project/lotus/api"
	apitypes "github.com/filecoin-project/lotus/api/types"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

// RSealDelegateAPI provides chain state access needed for CC sector scheduling.
type RSealDelegateAPI interface {
	StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (lotusapi.MinerInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (apitypes.NetworkVersion, error)
}

// RSealDelegate intercepts sectors before normal SDR processing and delegates
// them to remote providers. Uses the IAmBored pattern like SupraSeal's schedule().
//
// The schedule() callback only does fast DB operations to claim sectors.
// The expensive HTTP dance (CheckAvailable + SendOrder) happens in Do() so
// the scheduling loop is not blocked.
//
// When no existing unclaimed sectors are found, schedule() also creates new CC
// sectors from the sectors_cc_scheduler table for SPs that have enabled remote
// providers.
type RSealDelegate struct {
	db     *harmonydb.DB
	api    RSealDelegateAPI // optional, nil disables CC scheduling
	client *RSealClient

	lastScheduledWork bool // true if the last schedule() call found/created work
}

func NewRSealDelegate(db *harmonydb.DB, api RSealDelegateAPI, client *RSealClient) *RSealDelegate {
	return &RSealDelegate{
		db:     db,
		api:    api,
		client: client,
	}
}

// schedule is the IAmBored callback. It finds unclaimed sectors that have enabled
// providers and atomically claims them in the DB. No HTTP calls happen here —
// the expensive provider interaction is deferred to Do().
//
// When no existing unclaimed sectors are found, it also creates new CC sectors
// from the sectors_cc_scheduler table for SPs that have enabled remote providers.
func (d *RSealDelegate) schedule(taskFunc harmonytask.AddTaskFunc) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	d.lastScheduledWork = false

	// Find sectors ready for SDR that are not yet claimed by any task and
	// have no existing rseal_client_pipeline entry, but DO have an enabled provider.
	var sectors []struct {
		SpID                     int64  `db:"sp_id"`
		SectorNumber             int64  `db:"sector_number"`
		RegSealProof             int    `db:"reg_seal_proof"`
		ProviderID               int64  `db:"provider_id"`
		UserSectorDurationEpochs *int64 `db:"user_sector_duration_epochs"`
	}
	err := d.db.Select(ctx, &sectors, `
		SELECT s.sp_id, s.sector_number, s.reg_seal_proof, s.user_sector_duration_epochs, p.id AS provider_id
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
		// No existing unclaimed sectors — try to create CC sectors for remote delegation
		return d.scheduleCCRemote(ctx, taskFunc)
	}

	sector := sectors[0]

	// Atomically claim the sector in the DB. Do() will handle the HTTP calls.
	d.claimSectorForDelegation(taskFunc, sector.SpID, sector.SectorNumber, sector.ProviderID, sector.RegSealProof, sector.UserSectorDurationEpochs)
	d.lastScheduledWork = true

	return nil
}

// claimSectorForDelegation atomically creates the rseal_client_pipeline entry.
// The rseal_client_pipeline row is what prevents the local SDR poller and
// SupraSeal batch scheduler from claiming this sector (via LEFT JOIN / NOT EXISTS checks).
func (d *RSealDelegate) claimSectorForDelegation(taskFunc harmonytask.AddTaskFunc, spID, sectorNumber int64, providerID int64, regSealProof int, userDuration *int64) {
	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			INSERT INTO rseal_client_pipeline (sp_id, sector_number, provider_id, reg_seal_proof, user_sector_duration_epochs, task_id_sdr)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (sp_id, sector_number) DO NOTHING`,
			spID, sectorNumber, providerID, regSealProof, userDuration, id)
		if err != nil {
			return false, xerrors.Errorf("inserting rseal_client_pipeline: %w", err)
		}
		if n == 0 {
			return false, nil // already claimed
		}

		return true, nil
	})
}

// scheduleCCRemote creates new CC sectors from the sectors_cc_scheduler table
// for SPs that have enabled remote providers. It allocates a sector number,
// inserts into sectors_sdr_pipeline, creates the rseal_client_pipeline entry,
// and claims the sector — all in one transaction.
func (d *RSealDelegate) scheduleCCRemote(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	if d.api == nil {
		return nil // CC scheduling not configured
	}

	// Find enabled CC schedules for SPs that HAVE an enabled remote provider.
	var schedules []struct {
		SpID         int64 `db:"sp_id"`
		ToSeal       int64 `db:"to_seal"`
		DurationDays int64 `db:"duration_days"`
		ProviderID   int64 `db:"provider_id"`
	}
	err := d.db.Select(ctx, &schedules, `
		SELECT cs.sp_id, cs.to_seal, cs.duration_days, p.id AS provider_id
		FROM sectors_cc_scheduler cs
		JOIN rseal_client_providers p ON p.sp_id = cs.sp_id AND p.enabled = TRUE
		WHERE cs.enabled = TRUE
		  AND cs.to_seal > 0
		ORDER BY cs.sp_id
		LIMIT 1`)
	if err != nil {
		return xerrors.Errorf("querying cc_scheduler for remote: %w", err)
	}

	if len(schedules) == 0 {
		return nil
	}

	schedule := schedules[0]

	nv, err := d.api.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting network version: %w", err)
	}

	maddr, err := address.NewIDAddress(uint64(schedule.SpID))
	if err != nil {
		return xerrors.Errorf("creating miner address: %w", err)
	}

	mi, err := d.api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info for %s: %w", maddr, err)
	}

	spt, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, false)
	if err != nil {
		return xerrors.Errorf("getting seal proof type: %w", err)
	}

	userDuration := schedule.DurationDays * builtin.EpochsInDay
	if miner12.MaxSectorExpirationExtension < userDuration {
		return xerrors.Errorf("duration exceeds max allowed: %d > %d", userDuration, miner12.MaxSectorExpirationExtension)
	}
	if miner12.MinSectorExpiration > userDuration {
		return xerrors.Errorf("duration is too short: %d < %d", userDuration, miner12.MinSectorExpiration)
	}

	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		// Allocate one sector number
		sectorNumbers, err := seal.AllocateSectorNumbers(ctx, d.api, tx, maddr, 1)
		if err != nil {
			return false, xerrors.Errorf("allocating sector number: %w", err)
		}
		if len(sectorNumbers) != 1 {
			return false, xerrors.Errorf("expected 1 sector number, got %d", len(sectorNumbers))
		}
		sectorNum := sectorNumbers[0]

		// Only insert into rseal_client_pipeline — the sectors_sdr_pipeline row
		// will be created later by ApplyRemoteCompletion when the provider finishes
		// SDR+trees and the sealed data is ready for precommit.
		n, err := tx.Exec(`
			INSERT INTO rseal_client_pipeline (sp_id, sector_number, provider_id, reg_seal_proof, user_sector_duration_epochs, task_id_sdr)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (sp_id, sector_number) DO NOTHING`,
			schedule.SpID, int64(sectorNum), schedule.ProviderID, int(spt), userDuration, id)
		if err != nil {
			return false, xerrors.Errorf("inserting rseal_client_pipeline: %w", err)
		}
		if n == 0 {
			return false, nil // shouldn't happen for a freshly allocated sector
		}

		// Decrement to_seal
		_, err = tx.Exec(`UPDATE sectors_cc_scheduler SET to_seal = to_seal - 1 WHERE sp_id = $1 AND to_seal > 0`, schedule.SpID)
		if err != nil {
			return false, xerrors.Errorf("decrementing to_seal: %w", err)
		}

		log.Infow("CC scheduler: created remote CC sector",
			"sp_id", schedule.SpID,
			"sector", sectorNum,
			"proof", spt,
			"provider_id", schedule.ProviderID,
			"duration_days", schedule.DurationDays)

		return true, nil
	})

	d.lastScheduledWork = true
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

	// Order accepted. Clear task_id_sdr in rseal_client_pipeline so the poller
	// can create a RSealClientPoll task to monitor progress.
	_, err = d.db.Exec(ctx, `UPDATE rseal_client_pipeline SET task_id_sdr = NULL
		WHERE sp_id = $1 AND sector_number = $2 AND task_id_sdr = $3`,
		sector.SpID, sector.SectorNumber, taskID)
	if err != nil {
		return false, xerrors.Errorf("clearing delegate task_id_sdr: %w", err)
	}

	log.Infow("delegated sector to remote provider",
		"sp_id", sector.SpID,
		"sector", sector.SectorNumber,
		"provider", sector.ProviderURL)

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
		RetryWait:   taskhelp.RetryWaitLinear(20*time.Second, 0),
		IAmBored:    d.adaptiveSchedule(),
	}
}

// adaptiveSchedule returns a rate-limited schedule function that runs more
// frequently (1s) when work was found on the last call, and backs off to 15s
// when idle. This allows rapid CC sector creation when the scheduler is active.
func (d *RSealDelegate) adaptiveSchedule() func(harmonytask.AddTaskFunc) error {
	var lastCall time.Time
	var lk sync.Mutex

	return func(taskFunc harmonytask.AddTaskFunc) error {
		lk.Lock()
		defer lk.Unlock()

		interval := 15 * time.Second
		if d.lastScheduledWork {
			interval = 1 * time.Second
		}

		if time.Since(lastCall) < interval {
			return nil
		}

		defer func() {
			lastCall = time.Now()
		}()

		return d.schedule(taskFunc)
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
