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

// RSealClientPoll polls remote providers for completion status.
// This is a fallback mechanism - the primary path is the /complete callback
// from the provider. The poll task runs periodically to catch cases where
// the callback was missed.
type RSealClientPoll struct {
	db     *harmonydb.DB
	client *RSealClient
	sp     *RSealClientPoller
}

func NewRSealClientPoll(db *harmonydb.DB, client *RSealClient, sp *RSealClientPoller) *RSealClientPoll {
	return &RSealClientPoll{
		db:     db,
		client: client,
		sp:     sp,
	}
}

func (p *RSealClientPoll) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	const pollInterval = 30 * time.Second

	// Find the sector assigned to this poll task
	var sectors []struct {
		SpID          int64  `db:"sp_id"`
		SectorNumber  int64  `db:"sector_number"`
		RegSealProof  int    `db:"reg_seal_proof"`
		ProviderID    int64  `db:"provider_id"`
		ProviderURL   string `db:"provider_url"`
		ProviderToken string `db:"provider_token"`
	}

	err = p.db.Select(ctx, &sectors, `
		SELECT c.sp_id, c.sector_number, c.reg_seal_proof, c.provider_id, pr.provider_url, pr.provider_token
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers pr ON c.provider_id = pr.id
		WHERE c.task_id_sdr = $1 AND c.after_sdr = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("querying sector for poll task: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for poll task, got %d", len(sectors))
	}
	sector := sectors[0]

	// Poll the provider in a loop until completion, failure, or ownership loss
	for {
		statusResp, err := p.client.GetStatus(ctx, sector.ProviderURL, sector.ProviderToken, &sealmarket.StatusRequest{
			SpID:         sector.SpID,
			SectorNumber: sector.SectorNumber,
		})
		if err != nil {
			// HTTP error - log and retry within the loop after a sleep
			log.Warnw("remote seal poll: error polling provider, will retry",
				"sp_id", sector.SpID, "sector", sector.SectorNumber, "error", err)

			time.Sleep(pollInterval)

			if !stillOwned() {
				return false, xerrors.Errorf("yield")
			}
			continue
		}

		switch statusResp.State {
		case "complete":
			// Provider is done with SDR+trees. Apply the completion (ticket comes from status response).
			if err := sealmarket.ApplyRemoteCompletion(ctx, p.db, sector.SpID, sector.SectorNumber, sector.ProviderID,
				statusResp.TreeDCid, statusResp.TreeRCid, statusResp.TicketEpoch, statusResp.TicketValue); err != nil {
				return false, xerrors.Errorf("applying remote completion: %w", err)
			}

			log.Infow("remote seal poll: sector completed",
				"sp_id", sector.SpID, "sector", sector.SectorNumber,
				"tree_d_cid", statusResp.TreeDCid, "tree_r_cid", statusResp.TreeRCid)

			return true, nil

		case "failed":
			// Provider reports failure - mark the client pipeline as failed
			_, err := p.db.Exec(ctx, `
				UPDATE rseal_client_pipeline
				SET failed = TRUE, failed_at = NOW(), failed_reason = 'provider', failed_reason_msg = $3,
				    task_id_sdr = NULL
				WHERE sp_id = $1 AND sector_number = $2`,
				sector.SpID, sector.SectorNumber, statusResp.FailReason)
			if err != nil {
				return false, xerrors.Errorf("marking sector failed: %w", err)
			}

			// Also clear the task_ids in sectors_sdr_pipeline so it can be retried
			_, err = p.db.Exec(ctx, `
				UPDATE sectors_sdr_pipeline
				SET task_id_sdr = NULL, task_id_tree_d = NULL, task_id_tree_c = NULL, task_id_tree_r = NULL
				WHERE sp_id = $1 AND sector_number = $2`,
				sector.SpID, sector.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("clearing sector task ids: %w", err)
			}

			log.Warnw("remote seal poll: sector failed on provider",
				"sp_id", sector.SpID, "sector", sector.SectorNumber,
				"reason", statusResp.FailReason)

			return true, nil

		default:
			// Still in progress (pending, sdr, trees) - sleep and poll again
			log.Debugw("remote seal poll: sector still in progress",
				"sp_id", sector.SpID, "sector", sector.SectorNumber,
				"state", statusResp.State)

			time.Sleep(pollInterval)

			if !stillOwned() {
				return false, xerrors.Errorf("yield")
			}
		}
	}
}

func (p *RSealClientPoll) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (p *RSealClientPoll) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name:     "RSealClientPoll",
		CanYield: true,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 16 << 20, // 16 MiB - just HTTP calls
		},
		MaxFailures: 10,
		RetryWait:   taskhelp.RetryWaitLinear(30*time.Second, 0),
	}
}

func (p *RSealClientPoll) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.sp.pollers[pollerClientPoll].Set(taskFunc)
}

func (p *RSealClientPoll) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := p.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (p *RSealClientPoll) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
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

var _ = harmonytask.Reg(&RSealClientPoll{})
var _ harmonytask.TaskInterface = &RSealClientPoll{}
