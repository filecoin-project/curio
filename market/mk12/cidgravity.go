package mk12

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// CidGravityPayload defines the structure of the JSON payload for the POST request
type CidGravityPayload struct {
	Agent         string `json:"Agent"`
	FormatVersion string `json:"FormatVersion"`
	DealType      string `json:"DealType"`

	DealUUID           string `json:"DealUUID"`
	IsOffline          bool   `json:"IsOffline"`
	Size               int64  `json:"Size"`
	RemoveUnsealedCopy bool   `json:"RemoveUnsealedCopy"`
	SkipIPNIAnnounce   bool   `json:"SkipIPNIAnnounce"`

	ClientDealProposal struct {
		ClientSignature struct {
			Data []byte         `json:"Data"`
			Type crypto.SigType `json:"Type"`
		} `json:"ClientSignature"`
		Proposal market.DealProposal `json:"Proposal"`
	} `json:"ClientDealProposal"`

	DealDataRoot struct {
		CID string `json:"/"`
	} `json:"DealDataRoot"`

	FundsState struct {
		Collateral struct {
			Address address.Address `json:"Address"` // Garbage data
			Balance big.Int         `json:"Balance"` // Garbage data
		} `json:"Collateral"` // Garbage data
		Escrow struct {
			Available big.Int `json:"Available"`
			Locked    big.Int `json:"Locked"`
			Tagged    big.Int `json:"Tagged"` // Garbage data
		}
		PubMsg struct {
			Address address.Address `json:"Address"`
			Balance big.Int         `json:"Balance"`
		}
	}

	SealingPipelineState struct {
		DealStagingStates struct {
			AcceptedWaitingDownload int `json:"AcceptedWaitingDownload"`
			Downloading             int `json:"Downloading"`
			Publishing              int `json:"Publishing"`
			Sealing                 int `json:"Sealing"`
		} `json:"DealStagingStates"`
		Pipeline struct {
			IsSnap bool            `json:"IsSnap"`
			States []SealingStates `json:"States"`
		}
	}

	StorageState struct {
		Free           int64 `json:"Free"`
		Staged         int64 `json:"Staged"`
		Tagged         int64 `json:"Tagged"` // Garbage data
		TotalAvailable int64 `json:"TotalAvailable"`
	}
}

type SealingStates struct {
	Name    string `json:"Name"`
	Pending int64  `json:"Pending"`
	Running int64  `json:"Running"`
}

type cidGravityResponse struct {
	Decision                string `json:"decision"`
	CustomMessage           string `json:"customMessage"`
	ExternalMessage         string `json:"externalMessage"`
	InternalMessage         string `json:"internalMessage"`
	ExternalDecision        string `json:"externalDecision"`
	InternalDecision        string `json:"internalDecision"`
	MatchingAcceptanceLogic string `json:"matchingAcceptanceLogic"`
	MatchingPricing         string `json:"matchingPricing"`
	MatchingRule            int    `json:"matchingRule"`
}

const cidGravityUrl = "https://service.cidgravity.com/api/proposal/check"
const agentName = "curio"
const formatVersion = "2.3.0"

var commonHeaders = http.Header{
	"X-Agent":         []string{"curio-market-storage-filter"},
	"X-Agent-Version": []string{"1.0"},
	"Content-Type":    []string{"application/json"},
}

func (m *MK12) cidGravityCheck(ctx context.Context, deal *ProviderDealState) (bool, string, error) {

	data, err := m.prepareCidGravityPayload(ctx, deal)
	if err != nil {
		return false, "", xerrors.Errorf("Error preparing cid gravity payload: %v", err)
	}

	// Creating a new HTTP client
	client := &http.Client{}

	// Create the new request
	req, err := http.NewRequest("POST", cidGravityUrl, bytes.NewBuffer(data))
	if err != nil {
		return false, "", xerrors.Errorf("Error creating request: %v", err)
	}

	// Add necessary headers
	req.Header = commonHeaders
	req.Header.Set("Authorization", m.cfg.Market.StorageMarketConfig.MK12.CIDGravityToken)

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return false, "", xerrors.Errorf("Error making request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Errorf("Error closing response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		log.Errorf("cid gravity response status for dealID %s: %d", deal.DealUuid.String(), resp.StatusCode)
		if m.cfg.Market.StorageMarketConfig.MK12.DefaultCIDGravityAccept {
			return true, "", nil
		}
		return false, "deal rejected as CIDGravity service is not available", nil
	}

	body := new(bytes.Buffer)
	_, err = body.ReadFrom(resp.Body)
	if err != nil {
		return false, "", xerrors.Errorf("Error reading response body: %v", err)
	}

	response := cidGravityResponse{}
	err = json.Unmarshal(body.Bytes(), &response)

	if err != nil {
		return false, "", xerrors.Errorf("Error parsing response body: %v", err)
	}

	log.Debugw("cid gravity response",
		"dealId", deal.DealUuid.String(),
		"decision", response.Decision,
		"customMessage", response.CustomMessage,
		"externalMessage", response.ExternalMessage,
		"internalMessage", response.InternalMessage,
		"externalDecision", response.ExternalDecision,
		"internalDecision", response.InternalDecision,
		"matchingAcceptanceLogic", response.MatchingAcceptanceLogic,
		"matchingPricing", response.MatchingPricing,
		"matchingRule", response.MatchingRule)

	if response.Decision == "accept" {
		return true, "", nil
	}

	return false, fmt.Sprintf("%s. %s", response.ExternalMessage, response.CustomMessage), nil
}

func (m *MK12) prepareCidGravityPayload(ctx context.Context, deal *ProviderDealState) ([]byte, error) {
	data := CidGravityPayload{
		Agent:         agentName,
		FormatVersion: formatVersion,
		DealType:      "storage",
	}

	data.DealUUID = deal.DealUuid.String()
	data.IsOffline = deal.IsOffline
	data.Size = 0 // 0 by default for offline deals
	if !deal.IsOffline {
		data.Size = int64(deal.Transfer.Size)
	}

	data.ClientDealProposal.ClientSignature.Data = deal.ClientDealProposal.ClientSignature.Data
	data.ClientDealProposal.ClientSignature.Type = deal.ClientDealProposal.ClientSignature.Type
	data.ClientDealProposal.Proposal = deal.ClientDealProposal.Proposal
	data.DealDataRoot.CID = deal.DealDataRoot.String()

	// Fund details
	mbal, err := m.api.StateMarketBalance(ctx, deal.ClientDealProposal.Proposal.Provider, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	data.FundsState.Escrow.Tagged = big.NewInt(0)
	data.FundsState.Escrow.Locked = mbal.Locked
	data.FundsState.Escrow.Available = big.Sub(mbal.Escrow, mbal.Locked)

	data.FundsState.Collateral.Address = deal.ClientDealProposal.Proposal.Provider
	data.FundsState.Collateral.Balance = big.Sub(mbal.Escrow, mbal.Locked)

	mi, err := m.api.StateMinerInfo(ctx, deal.ClientDealProposal.Proposal.Provider, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting provider info: %w", err)
	}

	addr, af, err := m.as.AddressFor(ctx, m.api, deal.ClientDealProposal.Proposal.Provider, mi, api.DealPublishAddr, big.Zero(), big.Zero())
	if err != nil {
		return nil, xerrors.Errorf("selecting address for publishing deals: %w", err)
	}

	data.FundsState.PubMsg.Address = addr
	data.FundsState.PubMsg.Balance = af

	// Storage details
	type StorageUseStats struct {
		Available int64 `db:"available"`
		Capacity  int64 `db:"capacity"`
	}

	var stats []StorageUseStats

	err = m.db.Select(ctx, &stats, `SELECT SUM(available) as available, SUM(capacity) as capacity FROM storage_path WHERE can_seal = true`)
	if err != nil {
		return nil, err
	}

	if len(stats) == 0 {
		return nil, xerrors.Errorf("expected 1 row, got 0")
	}

	data.StorageState.TotalAvailable = stats[0].Available
	data.StorageState.Free = stats[0].Capacity

	data.StorageState.Staged = 0
	data.StorageState.Tagged = 0

	// Pipeline details (mostly just fake)

	var pipelineStats []struct {
		NotStartedOffline          int `db:"not_started_offline"`
		NotStartedOnline           int `db:"not_started_online"`
		AfterCommpNotAfterPsd      int `db:"after_commp_not_after_psd"`
		AfterFindDealSectorNotNull int `db:"after_find_deal_sector_not_null"`
	}

	err = m.db.Select(ctx, &stats, `SELECT 
										COUNT(*) FILTER (WHERE started = FALSE AND offline = TRUE) AS not_started_offline,
										COUNT(*) FILTER (WHERE started = FALSE AND offline = FALSE) AS not_started_online,
										COUNT(*) FILTER (WHERE after_commp = TRUE AND after_psd = FALSE) AS after_commp_not_after_psd,
										COUNT(*) FILTER (WHERE after_find_deal = TRUE AND sector IS NOT NULL) AS after_find_deal_sector_not_null
									FROM market_mk12_deal_pipeline;
									`)
	if err != nil {
		return nil, xerrors.Errorf("failed to run deal pipeline query: %w", err)
	}

	if len(pipelineStats) == 0 {
		return nil, xerrors.Errorf("expected 1 row, got 0")
	}

	data.SealingPipelineState.DealStagingStates.AcceptedWaitingDownload = pipelineStats[0].NotStartedOffline
	data.SealingPipelineState.DealStagingStates.Downloading = pipelineStats[0].NotStartedOnline
	data.SealingPipelineState.DealStagingStates.Publishing = pipelineStats[0].AfterCommpNotAfterPsd
	data.SealingPipelineState.DealStagingStates.Sealing = pipelineStats[0].AfterFindDealSectorNotNull
	data.SealingPipelineState.Pipeline.IsSnap = m.cfg.Ingest.DoSnap

	if m.cfg.Ingest.DoSnap {
		var cts []struct {
			Total int64 `db:"total"`

			EncodePending int64 `db:"encode_pending"`
			EncodeRunning int64 `db:"encode_running"`

			ProvePending int64 `db:"prove_pending"`
			ProveRunning int64 `db:"prove_running"`

			SubmitPending int64 `db:"submit_pending"`
			SubmitRunning int64 `db:"submit_running"`

			MoveStoragePending int64 `db:"move_storage_pending"`
			MoveStorageRunning int64 `db:"move_storage_running"`
		}

		err = m.db.Select(ctx, &cts, `WITH pipeline_data AS (
											SELECT sp.*,
												   et.owner_id AS encode_owner, 
												   pt.owner_id AS prove_owner,
												   st.owner_id AS submit_owner,
												   mt.owner_id AS move_storage_owner
											FROM sectors_snap_pipeline sp
											LEFT JOIN harmony_task et ON et.id = sp.task_id_encode
											LEFT JOIN harmony_task pt ON pt.id = sp.task_id_prove
											LEFT JOIN harmony_task st ON st.id = sp.task_id_submit
											LEFT JOIN harmony_task mt ON mt.id = sp.task_id_move_storage
											WHERE after_move_storage = false
											)
											SELECT
												COUNT(*) AS total,
											
												-- Encode stage
												COUNT(*) FILTER (WHERE after_encode = false AND task_id_encode IS NOT NULL AND encode_owner IS NULL) AS encode_pending,
												COUNT(*) FILTER (WHERE after_encode = false AND task_id_encode IS NOT NULL AND encode_owner IS NOT NULL) AS encode_running,
											
												-- Prove stage
												COUNT(*) FILTER (WHERE after_encode = true AND after_prove = false AND task_id_prove IS NOT NULL AND prove_owner IS NULL) AS prove_pending,
												COUNT(*) FILTER (WHERE after_encode = true AND after_prove = false AND task_id_prove IS NOT NULL AND prove_owner IS NOT NULL) AS prove_running,
											
												-- Submit stage
												COUNT(*) FILTER (WHERE after_prove = true AND after_submit = false AND task_id_submit IS NOT NULL AND submit_owner IS NULL) AS submit_pending,
												COUNT(*) FILTER (WHERE after_prove = true AND after_submit = false AND task_id_submit IS NOT NULL AND submit_owner IS NOT NULL) AS submit_running,
											
												-- Move Storage stage
												COUNT(*) FILTER (WHERE after_submit = true AND after_move_storage = false AND task_id_move_storage IS NOT NULL AND move_storage_owner IS NULL) AS move_storage_pending,
												COUNT(*) FILTER (WHERE after_submit = true AND after_move_storage = false AND task_id_move_storage IS NOT NULL AND move_storage_owner IS NOT NULL) AS move_storage_running
											FROM pipeline_data`)
		if err != nil {
			return nil, xerrors.Errorf("failed to run snap pipeline stage query: %w", err)
		}

		if len(cts) == 0 {
			return nil, xerrors.Errorf("expected 1 row, got 0")
		}

		ct := cts[0]

		data.SealingPipelineState.Pipeline.States = []SealingStates{
			{
				Name:    "Encode",
				Running: ct.EncodeRunning,
				Pending: ct.EncodePending,
			},
			{
				Name:    "ProveUpdate",
				Running: ct.ProveRunning,
				Pending: ct.ProvePending,
			},
			{
				Name:    "SubmitUpdate",
				Running: ct.SubmitRunning,
				Pending: ct.SubmitPending,
			},
			{
				Name:    "MoveStorage",
				Running: ct.MoveStorageRunning,
				Pending: ct.MoveStoragePending,
			},
		}

	} else {
		var cts []struct {
			Total int64 `db:"total"`

			SDRPending          int64 `db:"sdr_pending"`
			SDRRunning          int64 `db:"sdr_running"`
			TreesPending        int64 `db:"trees_pending"`
			TreesRunning        int64 `db:"trees_running"`
			PrecommitMsgPending int64 `db:"precommit_msg_pending"`
			PrecommitMsgRunning int64 `db:"precommit_msg_running"`
			WaitSeedPending     int64 `db:"wait_seed_pending"`
			WaitSeedRunning     int64 `db:"wait_seed_running"`
			PoRepPending        int64 `db:"porep_pending"`
			PoRepRunning        int64 `db:"porep_running"`
			CommitMsgPending    int64 `db:"commit_msg_pending"`
			CommitMsgRunning    int64 `db:"commit_msg_running"`
		}

		err = m.db.Select(ctx, &cts, `WITH pipeline_data AS (
												SELECT
													sp.*,
													sdrt.owner_id AS sdr_owner,
													tdt.owner_id AS tree_d_owner,
													tct.owner_id AS tree_c_owner,
													trt.owner_id AS tree_r_owner,
													pmt.owner_id AS precommit_msg_owner,
													pot.owner_id AS porep_owner,
													cmt.owner_id AS commit_msg_owner
												FROM sectors_sdr_pipeline sp
												LEFT JOIN harmony_task sdrt ON sdrt.id = sp.task_id_sdr
												LEFT JOIN harmony_task tdt ON tdt.id = sp.task_id_tree_d
												LEFT JOIN harmony_task tct ON tct.id = sp.task_id_tree_c
												LEFT JOIN harmony_task trt ON trt.id = sp.task_id_tree_r
												LEFT JOIN harmony_task pmt ON pmt.id = sp.task_id_precommit_msg
												LEFT JOIN harmony_task pot ON pot.id = sp.task_id_porep
												LEFT JOIN harmony_task cmt ON cmt.id = sp.task_id_commit_msg
											),
											stages AS (
												SELECT
													*,
													-- Determine stage membership booleans
													(after_sdr = false) AS at_sdr,
													(
													  after_sdr = true
													  AND (
														after_tree_d = false
														OR after_tree_c = false
														OR after_tree_r = false
													  )
													) AS at_trees,
													(after_tree_r = true AND after_precommit_msg = false) AS at_precommit_msg,
													(after_precommit_msg_success = true AND seed_epoch > $1) AS at_wait_seed,
													(after_porep = false AND after_precommit_msg_success = true AND seed_epoch < $1) AS at_porep,
													(after_commit_msg_success = false AND after_porep = true) AS at_commit_msg,
													(after_commit_msg_success = true) AS at_done,
													(failed = true) AS at_failed
												FROM pipeline_data
											)
											SELECT
												-- Total active pipelines: those not done and not failed
												COUNT(*) FILTER (WHERE NOT at_done AND NOT at_failed) AS total,
											
												-- SDR stage pending/running
												COUNT(*) FILTER (WHERE at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NULL) AS sdr_pending,
												COUNT(*) FILTER (WHERE at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NOT NULL) AS sdr_running,
											
												-- Trees stage pending/running
												-- A pipeline at the trees stage may have up to three tasks.
												-- Pending if ANY tree task that is not completed is present with no owner
												-- Running if ANY tree task that is not completed is present with owner
												COUNT(*) FILTER (
													WHERE at_trees
													  AND (
														  (task_id_tree_d IS NOT NULL AND tree_d_owner IS NULL AND after_tree_d = false)
													   OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NULL AND after_tree_c = false)
													   OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NULL AND after_tree_r = false)
													  )
												) AS trees_pending,
												COUNT(*) FILTER (
													WHERE at_trees
													  AND (
														  (task_id_tree_d IS NOT NULL AND tree_d_owner IS NOT NULL AND after_tree_d = false)
													   OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NOT NULL AND after_tree_c = false)
													   OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NOT NULL AND after_tree_r = false)
													  )
												) AS trees_running,
											
												-- PrecommitMsg stage
												COUNT(*) FILTER (WHERE at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NULL) AS precommit_msg_pending,
												COUNT(*) FILTER (WHERE at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NOT NULL) AS precommit_msg_running,
											
												-- WaitSeed stage (no tasks)
												0 AS wait_seed_pending,
												0 AS wait_seed_running,
											
												-- PoRep stage
												COUNT(*) FILTER (WHERE at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NULL) AS porep_pending,
												COUNT(*) FILTER (WHERE at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NOT NULL) AS porep_running,
											
												-- CommitMsg stage
												COUNT(*) FILTER (WHERE at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NULL) AS commit_msg_pending,
												COUNT(*) FILTER (WHERE at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NOT NULL) AS commit_msg_running
											
											FROM stages`)
		if err != nil {
			return nil, xerrors.Errorf("failed to run sdr pipeline stage query: %w", err)
		}

		if len(cts) == 0 {
			return nil, xerrors.Errorf("expected 1 row, got 0")
		}

		ct := cts[0]

		data.SealingPipelineState.Pipeline.States = []SealingStates{
			{
				Name:    "SDR",
				Running: ct.SDRRunning,
				Pending: ct.SDRPending,
			},
			{
				Name:    "Trees",
				Running: ct.TreesRunning,
				Pending: ct.TreesPending,
			},
			{
				Name:    "PrecommitMsg",
				Running: ct.PrecommitMsgRunning,
				Pending: ct.PrecommitMsgPending,
			},
			{
				Name:    "WaitSeed",
				Running: ct.WaitSeedRunning,
				Pending: ct.WaitSeedPending,
			},
			{
				Name:    "PoRep",
				Running: ct.PoRepRunning,
				Pending: ct.PoRepPending,
			},
			{
				Name:    "CommitMsg",
				Running: ct.CommitMsgRunning,
				Pending: ct.CommitMsgPending,
			},
		}
	}

	return json.Marshal(data)
}
