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

type RSealProviderTicket struct {
	db *harmonydb.DB
	sp *RSealProviderPoller

	httpClient *http.Client
}

func NewProviderTicketTask(db *harmonydb.DB, sp *RSealProviderPoller) *RSealProviderTicket {
	return &RSealProviderTicket{
		db: db,
		sp: sp,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *RSealProviderTicket) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Find the sector assigned to this task.
	// The ticket fetch task reuses the task_id_sdr column, with ticket_epoch IS NULL
	// distinguishing it from a real SDR task.
	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`
		PartnerID    int64 `db:"partner_id"`
	}

	err = t.db.Select(ctx, &sectors, `SELECT sp_id, sector_number, partner_id
		FROM rseal_provider_pipeline
		WHERE task_id_sdr = $1 AND ticket_epoch IS NULL`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector for ticket fetch: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for ticket fetch, got %d", len(sectors))
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

	// Fetch ticket from the client via HTTP
	ticketReq := sealmarket.TicketRequest{
		PartnerToken: partner.PartnerToken,
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
	}

	ticketResp, err := t.fetchTicket(ctx, partner.PartnerURL, ticketReq)
	if err != nil {
		return false, xerrors.Errorf("fetching ticket from client: %w", err)
	}

	if ticketResp.TicketEpoch == 0 || len(ticketResp.TicketValue) == 0 {
		return false, xerrors.Errorf("invalid ticket response: epoch=%d, value_len=%d", ticketResp.TicketEpoch, len(ticketResp.TicketValue))
	}

	// Store the ticket and clear task_id_sdr so the real SDR task can be assigned
	n, err := t.db.Exec(ctx, `UPDATE rseal_provider_pipeline
		SET ticket_epoch = $1, ticket_value = $2, task_id_sdr = NULL
		WHERE sp_id = $3 AND sector_number = $4 AND task_id_sdr = $5`,
		ticketResp.TicketEpoch, ticketResp.TicketValue, sector.SpID, sector.SectorNumber, taskID)
	if err != nil {
		return false, xerrors.Errorf("storing ticket: %w", err)
	}
	if n != 1 {
		return false, xerrors.Errorf("expected to update 1 row storing ticket, updated %d", n)
	}

	log.Infow("ticket fetched for remote seal sector",
		"sp", sector.SpID,
		"sector", sector.SectorNumber,
		"ticketEpoch", ticketResp.TicketEpoch)

	return true, nil
}

func (t *RSealProviderTicket) fetchTicket(ctx context.Context, partnerURL string, req sealmarket.TicketRequest) (*sealmarket.TicketResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, xerrors.Errorf("marshaling ticket request: %w", err)
	}

	url := fmt.Sprintf("%s/remoteseal/delegated/v0/ticket", partnerURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, xerrors.Errorf("creating ticket request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, xerrors.Errorf("sending ticket request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, xerrors.Errorf("ticket request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var ticketResp sealmarket.TicketResponse
	if err := json.NewDecoder(resp.Body).Decode(&ticketResp); err != nil {
		return nil, xerrors.Errorf("decoding ticket response: %w", err)
	}

	return &ticketResp, nil
}

func (t *RSealProviderTicket) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	// Ticket fetch is a lightweight HTTP call; accept all offered tasks.
	return ids, nil
}

func (t *RSealProviderTicket) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(4),
		Name: "RSealProvTicket",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 100,
		RetryWait:   taskhelp.RetryWaitLinear(30*time.Second, 10*time.Second),
	}
}

func (t *RSealProviderTicket) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.sp.pollers[pollerProvTicketFetch].Set(taskFunc)
}

func (t *RSealProviderTicket) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := t.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (t *RSealProviderTicket) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(), `SELECT sp_id, sector_number
		FROM rseal_provider_pipeline
		WHERE task_id_sdr = $1 AND ticket_epoch IS NULL`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, xerrors.Errorf("getting sector id for ticket task: %w", err)
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealProviderTicket{})
var _ harmonytask.TaskInterface = &RSealProviderTicket{}
