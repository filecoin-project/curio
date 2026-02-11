package remoteseal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/market/sealmarket"
)

type RSealProviderNotify struct {
	db *harmonydb.DB
	sp *RSealProviderPoller

	httpClient *http.Client
}

func NewProviderNotifyTask(db *harmonydb.DB, sp *RSealProviderPoller) *RSealProviderNotify {
	return &RSealProviderNotify{
		db: db,
		sp: sp,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (t *RSealProviderNotify) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Find the sector assigned to this task
	var sectors []struct {
		SpID         int64  `db:"sp_id"`
		SectorNumber int64  `db:"sector_number"`
		PartnerID    int64  `db:"partner_id"`
		TreeDCid     string `db:"tree_d_cid"`
		TreeRCid     string `db:"tree_r_cid"`
	}

	err = t.db.Select(ctx, &sectors, `SELECT sp_id, sector_number, partner_id, tree_d_cid, tree_r_cid
		FROM rseal_provider_pipeline
		WHERE task_id_notify_client = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector for notify: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for notify, got %d", len(sectors))
	}
	sector := sectors[0]

	// Look up the partner URL and token
	var partners []struct {
		PartnerURL   string `db:"partner_url"`
		PartnerToken string `db:"partner_token"`
	}

	err = t.db.Select(ctx, &partners, `SELECT partner_url, partner_token
		FROM rseal_delegated_partners
		WHERE id = $1`, sector.PartnerID)
	if err != nil {
		return false, xerrors.Errorf("getting partner info: %w", err)
	}

	if len(partners) != 1 {
		return false, xerrors.Errorf("expected 1 partner, got %d", len(partners))
	}
	partner := partners[0]

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Notify the client that SDR+trees are complete
	notification := sealmarket.CompleteNotification{
		PartnerToken: partner.PartnerToken,
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
		TreeDCid:     sector.TreeDCid,
		TreeRCid:     sector.TreeRCid,
	}

	err = t.sendCompleteNotification(ctx, partner.PartnerURL, notification)
	if err != nil {
		return false, xerrors.Errorf("sending complete notification: %w", err)
	}

	// Mark notification as done and set the cleanup timeout
	n, err := t.db.Exec(ctx, `UPDATE rseal_provider_pipeline
		SET after_notify_client = TRUE, task_id_notify_client = NULL,
		    cleanup_timeout = NOW() + INTERVAL '72 hours'
		WHERE sp_id = $1 AND sector_number = $2 AND task_id_notify_client = $3`,
		sector.SpID, sector.SectorNumber, taskID)
	if err != nil {
		return false, xerrors.Errorf("updating notify status: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row for notify, updated %d", n)
	}

	log.Infow("notified client of remote seal completion",
		"sp", sector.SpID,
		"sector", sector.SectorNumber,
		"treeDCid", sector.TreeDCid,
		"treeRCid", sector.TreeRCid)

	return true, nil
}

func (t *RSealProviderNotify) sendCompleteNotification(ctx context.Context, partnerURL string, notification sealmarket.CompleteNotification) error {
	body, err := json.Marshal(notification)
	if err != nil {
		return xerrors.Errorf("marshaling complete notification: %w", err)
	}

	url := fmt.Sprintf("%s/remoteseal/delegated/v0/complete", partnerURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return xerrors.Errorf("creating complete notification request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return xerrors.Errorf("sending complete notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("complete notification failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (t *RSealProviderNotify) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// Notification is a lightweight HTTP call; accept all offered tasks.
	return ids, nil
}

func (t *RSealProviderNotify) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(4),
		Name: "RSealProvNotify",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 100,
		RetryWait:   taskhelp.RetryWaitLinear(60*time.Second, 30*time.Second),
	}
}

func (t *RSealProviderNotify) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.sp.pollers[pollerProvNotifyClient].Set(taskFunc)
}

func (t *RSealProviderNotify) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *RSealProviderNotify) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number
		FROM rseal_provider_pipeline
		WHERE task_id_notify_client = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, xerrors.Errorf("getting sector id for notify task: %w", err)
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealProviderNotify{})
var _ harmonytask.TaskInterface = &RSealProviderNotify{}
