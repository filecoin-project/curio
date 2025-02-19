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

const FormatVersion = "1.0"

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
			IsSnap bool          `json:"IsSnap"`
			States SealingStates `json:"States"`
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
	SDRPending          int64 `db:"sdr_pending" json:"SDRPending,omitempty"`
	SDRRunning          int64 `db:"sdr_running"  json:"SDRRunning,omitempty"`
	SDRFailed           int64 `db:"sdr_failed" json:"SDRFailed,omitempty"`
	TreesPending        int64 `db:"trees_pending" json:"TreesPending,omitempty"`
	TreesRunning        int64 `db:"trees_running" json:"TreesRunning,omitempty"`
	TreesFailed         int64 `db:"trees_failed" json:"TreesFailed,omitempty"`
	PrecommitMsgPending int64 `db:"precommit_msg_pending" json:"PrecommitMsgPending,omitempty"`
	PrecommitMsgRunning int64 `db:"precommit_msg_running" json:"PrecommitMsgRunning,omitempty"`
	PrecommitMsgFailed  int64 `db:"precommit_msg_failed" json:"PrecommitMsgFailed,omitempty"`
	WaitSeedPending     int64 `db:"wait_seed_pending" json:"WaitSeedPending,omitempty"`
	WaitSeedRunning     int64 `db:"wait_seed_running" json:"WaitSeedRunning,omitempty"`
	WaitSeedFailed      int64 `db:"wait_seed_failed" json:"WaitSeedFailed,omitempty"`
	PoRepPending        int64 `db:"porep_pending" json:"PoRepPending,omitempty"`
	PoRepRunning        int64 `db:"porep_running" json:"PoRepRunning,omitempty"`
	PoRepFailed         int64 `db:"porep_failed" json:"PoRepFailed,omitempty"`
	CommitMsgPending    int64 `db:"commit_msg_pending" json:"CommitMsgPending,omitempty"`
	CommitMsgRunning    int64 `db:"commit_msg_running" json:"CommitMsgRunning,omitempty"`
	CommitMsgFailed     int64 `db:"commit_msg_failed" json:"CommitMsgFailed,omitempty"`
	EncodeRunning       int64 `db:"encode_running" json:"EncodeRunning,omitempty"`
	EncodePending       int64 `db:"encode_pending" json:"EncodePending,omitempty"`
	EncodeFailed        int64 `db:"encode_failed" json:"EncodeFailed,omitempty"`
	ProveRunning        int64 `db:"prove_running" json:"ProveRunning,omitempty"`
	ProvePending        int64 `db:"prove_pending" json:"ProvePending,omitempty"`
	ProveFailed         int64 `db:"prove_failed" json:"ProveFailed,omitempty"`
	SubmitRunning       int64 `db:"submit_running" json:"SubmitRunning,omitempty"`
	SubmitPending       int64 `db:"submit_pending" json:"SubmitPending,omitempty"`
	SubmitFailed        int64 `db:"submit_failed" json:"SubmitFailed,omitempty"`
	MoveStorageRunning  int64 `db:"move_storage_running" json:"MoveStorageRunning,omitempty"`
	MoveStoragePending  int64 `db:"move_storage_pending" json:"MoveStoragePending,omitempty"`
	MoveStorageFailed   int64 `db:"move_storage_failed" json:"MoveStorageFailed,omitempty"`
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

const cidGravityDealCheckUrl = "https://service.cidgravity.com/api/proposal/check"
const cidGravityMinerCheckUrl = "https://service.cidgravity.com/private/v1/miner-status-checker/check"
const cidGravityMinerCheckLabel = "cidg-miner-status-check"
const agentName = "curio"

var commonHeaders = http.Header{
	"X-CIDgravity-Agent":   []string{"CIDgravity-storage-Connector"},
	"X-CIDgravity-Version": []string{"1.0"},
	"Content-Type":         []string{"application/json"},
}

func (m *MK12) cidGravityCheck(ctx context.Context, deal *ProviderDealState) (bool, string, error) {

	data, err := m.prepareCidGravityPayload(ctx, deal)
	if err != nil {
		return false, "", xerrors.Errorf("Error preparing cid gravity payload: %v", err)
	}

	// Creating a new HTTP client
	client := &http.Client{}

	// Create the new request
	var req *http.Request
	if deal.ClientDealProposal.Proposal.Label.IsString() {
		lableStr, err := deal.ClientDealProposal.Proposal.Label.ToString()
		if err != nil {
			return false, "", xerrors.Errorf("Error getting label string: %v", err)
		}
		if lableStr == cidGravityMinerCheckLabel {
			req, err = http.NewRequest("POST", cidGravityMinerCheckUrl, bytes.NewBuffer(data))
			if err != nil {
				return false, "", xerrors.Errorf("Error creating request: %v", err)
			}
		}
	} else {
		req, err = http.NewRequest("POST", cidGravityDealCheckUrl, bytes.NewBuffer(data))
		if err != nil {
			return false, "", xerrors.Errorf("Error creating request: %v", err)
		}
	}

	// Add necessary headers
	req.Header = commonHeaders
	req.Header.Set("X-API-KEY", m.cfg.Market.StorageMarketConfig.MK12.CIDGravityToken)
	req.Header.Set("X-Address-ID", deal.ClientDealProposal.Proposal.Provider.String())
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
		FormatVersion: FormatVersion,
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
		var cts []SealingStates

		err = m.db.Select(ctx, &cts, `WITH pipeline_data AS (
											SELECT sp.*,
												   et.owner_id AS encode_owner, 
												   pt.owner_id AS prove_owner,
												   st.owner_id AS submit_owner,
												   mt.owner_id AS move_storage_owner

													(sp.task_id_encode IS NOT NULL AND et.id IS NULL) AS encode_missing,
													(sp.task_id_prove IS NOT NULL AND pt.id IS NULL) AS prove_missing,
													(sp.task_id_submit IS NOT NULL AND st.id IS NULL) AS submit_missing,
													(sp.task_id_move_storage IS NOT NULL AND mt.id IS NULL) AS move_storage_missing


											FROM sectors_snap_pipeline sp
											LEFT JOIN harmony_task et ON et.id = sp.task_id_encode
											LEFT JOIN harmony_task pt ON pt.id = sp.task_id_prove
											LEFT JOIN harmony_task st ON st.id = sp.task_id_submit
											LEFT JOIN harmony_task mt ON mt.id = sp.task_id_move_storage
											WHERE after_move_storage = false
											)
											SELECT
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

												COUNT(*) FILTER (WHERE encode_missing) AS encode_failed,
												COUNT(*) FILTER (WHERE prove_missing) AS prove_failed,
												COUNT(*) FILTER (WHERE submit_missing) AS submit_failed,
												COUNT(*) FILTER (WHERE move_storage_missing) AS move_storage_failed

											FROM pipeline_data`)
		if err != nil {
			return nil, xerrors.Errorf("failed to run snap pipeline stage query: %w", err)
		}

		if len(cts) == 0 {
			return nil, xerrors.Errorf("expected 1 row, got 0")
		}

		data.SealingPipelineState.Pipeline.States = cts[0]

	} else {

		var cts []SealingStates

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

													-- Detect missing task entries (task exists in pipeline but not in harmony_task)
													(sp.task_id_sdr IS NOT NULL AND sdrt.id IS NULL) AS sdr_missing,
													(
														(sp.task_id_tree_d IS NOT NULL AND tdt.id IS NULL) OR
														(sp.task_id_tree_c IS NOT NULL AND tct.id IS NULL) OR
														(sp.task_id_tree_r IS NOT NULL AND trt.id IS NULL)
													) AS trees_missing,
													(sp.task_id_precommit_msg IS NOT NULL AND pmt.id IS NULL) AS precommit_msg_missing,
													(sp.task_id_porep IS NOT NULL AND pot.id IS NULL) AS porep_missing,
													(sp.task_id_commit_msg IS NOT NULL AND cmt.id IS NULL) AS commit_msg_missing
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

												-- Failure Count for Missing Tasks
												COUNT(*) FILTER (WHERE sdr_missing) AS sdr_failed,
												COUNT(*) FILTER (WHERE trees_missing) AS trees_failed,
												COUNT(*) FILTER (WHERE precommit_msg_missing) AS precommit_msg_failed,
												COUNT(*) FILTER (WHERE porep_missing) AS porep_failed,
												COUNT(*) FILTER (WHERE commit_msg_missing) AS commit_msg_failed
											
											FROM stages`)
		if err != nil {
			return nil, xerrors.Errorf("failed to run sdr pipeline stage query: %w", err)
		}

		if len(cts) == 0 {
			return nil, xerrors.Errorf("expected 1 row, got 0")
		}

		data.SealingPipelineState.Pipeline.States = cts[0]
	}

	return json.Marshal(data)
}
