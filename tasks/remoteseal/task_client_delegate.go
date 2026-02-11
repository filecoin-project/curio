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
// providers, checks availability with each provider, and if an order is accepted,
// atomically claims the sector in both rseal_client_pipeline and sectors_sdr_pipeline.
func (d *RSealDelegate) schedule(taskFunc harmonytask.AddTaskFunc) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: Find sectors ready for SDR that are not yet claimed by any task and
	// have no existing rseal_client_pipeline entry.
	var sectors []candidateSector
	err := d.db.Select(ctx, &sectors, `
		SELECT sp_id, sector_number, reg_seal_proof
		FROM sectors_sdr_pipeline
		WHERE after_sdr = FALSE
		  AND task_id_sdr IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM rseal_client_pipeline c
			WHERE c.sp_id = sectors_sdr_pipeline.sp_id
			  AND c.sector_number = sectors_sdr_pipeline.sector_number
		  )
		LIMIT 10`)
	if err != nil {
		return xerrors.Errorf("finding candidate sectors: %w", err)
	}

	if len(sectors) == 0 {
		return nil
	}

	// Step 2: For each sector, try to find an available provider and delegate.
	for _, sector := range sectors {
		var providers []availableProvider
		err := d.db.Select(ctx, &providers, `
			SELECT id, provider_url, provider_token
			FROM rseal_client_providers
			WHERE sp_id = $1 AND enabled = TRUE`, sector.SpID)
		if err != nil {
			log.Errorw("failed to query providers", "sp_id", sector.SpID, "error", err)
			continue
		}

		if len(providers) == 0 {
			continue
		}

		// Try each provider for this sector
		delegated := false
		for _, prov := range providers {
			if delegated {
				break
			}

			// Check availability (HTTP call, outside transaction)
			availCtx, availCancel := context.WithTimeout(ctx, 5*time.Second)
			availResp, err := d.client.CheckAvailable(availCtx, prov.URL, prov.Token)
			availCancel()
			if err != nil {
				log.Warnw("provider availability check failed", "provider", prov.URL, "error", err)
				continue
			}
			if !availResp.Available {
				continue
			}

			slotToken := availResp.SlotToken

			// Send order (HTTP call, outside transaction - idempotent)
			orderResp, err := d.client.SendOrder(ctx, prov.URL, prov.Token, &sealmarket.OrderRequest{
				SlotToken:    slotToken,
				SpID:         sector.SpID,
				SectorNumber: sector.SectorNumber,
				RegSealProof: sector.RegSealProof,
			})
			if err != nil {
				log.Warnw("provider order failed", "provider", prov.URL, "error", err)
				continue
			}
			if !orderResp.Accepted {
				log.Infow("provider rejected order", "provider", prov.URL, "reason", orderResp.RejectReason,
					"sp_id", sector.SpID, "sector", sector.SectorNumber)
				continue
			}

			// Step 3: Order accepted - atomically claim the sector.
			provID := prov.ID
			sectorCopy := sector
			taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
				// Insert into rseal_client_pipeline
				n, err := tx.Exec(`
					INSERT INTO rseal_client_pipeline (sp_id, sector_number, provider_id, reg_seal_proof)
					VALUES ($1, $2, $3, $4)
					ON CONFLICT (sp_id, sector_number) DO NOTHING`,
					sectorCopy.SpID, sectorCopy.SectorNumber, provID, sectorCopy.RegSealProof)
				if err != nil {
					return false, xerrors.Errorf("inserting rseal_client_pipeline: %w", err)
				}
				if n == 0 {
					// Already exists - someone else claimed it
					return false, nil
				}

				// Claim the sector in sectors_sdr_pipeline by setting all SDR/tree task_ids
				// to this task's ID. This prevents the local SDR poller from assigning tasks.
				n, err = tx.Exec(`
					UPDATE sectors_sdr_pipeline
					SET task_id_sdr = $1, task_id_tree_d = $1, task_id_tree_c = $1, task_id_tree_r = $1
					WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL`,
					id, sectorCopy.SpID, sectorCopy.SectorNumber)
				if err != nil {
					return false, xerrors.Errorf("claiming sector in sdr_pipeline: %w", err)
				}
				if n != 1 {
					// Someone else claimed it in sectors_sdr_pipeline
					return false, nil
				}

				return true, nil
			})

			delegated = true
			log.Infow("delegated sector to remote provider",
				"sp_id", sector.SpID,
				"sector", sector.SectorNumber,
				"provider", prov.URL)
		}
	}

	return nil
}

func (d *RSealDelegate) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	// The RSealDelegate task has no Do work. All work happens in the IAmBored/schedule
	// callback which creates the task atomically. Once the task is created (order sent,
	// pipeline entries made), it completes immediately.
	//
	// The task_id set in sectors_sdr_pipeline will be cleaned up by harmonytask when
	// this task completes (task is deleted from harmony_task). The complete notification
	// from the provider (or the poll task) will set after_sdr=TRUE and clear task_ids.

	// However, the task can actually be scheduled - that means the taskFunc callback
	// returned true and the task was created. At this point, the delegation is done.

	// When this task completes, harmonytask deletes the task entry from harmony_task.
	// sectors_sdr_pipeline still has our old task_id values set in task_id_sdr etc.
	// The SDR poller sees task_id_sdr is non-null so it won't re-assign.
	// The /complete callback or RSealClientPoll will eventually set after_* = TRUE
	// and clear the task_ids.

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
