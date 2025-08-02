package mk12

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/lotus/api"
)

const FormatVersion = "1.0"

// CidGravityPayload defines the structure of the JSON payload for the POST request
type CidGravityPayload struct {
	Agent         string `json:"Agent"`
	FormatVersion string `json:"FormatVersion"`
	DealType      string `json:"DealType"`

	DealTransferType   string `json:"DealTransferType"`
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
			IsSnap            bool                        `json:"IsSnap"`
			UnderBackPressure bool                        `json:"UnderBackPressure"`
			States            map[string]map[string]int64 `json:"States"`
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
	SDRPending          int64 `db:"sdr_pending"`
	SDRRunning          int64 `db:"sdr_running"`
	SDRFailed           int64 `db:"sdr_failed"`
	TreesPending        int64 `db:"trees_pending"`
	TreesRunning        int64 `db:"trees_running"`
	TreesFailed         int64 `db:"trees_failed"`
	PrecommitMsgPending int64 `db:"precommit_msg_pending"`
	PrecommitMsgRunning int64 `db:"precommit_msg_running"`
	PrecommitMsgFailed  int64 `db:"precommit_msg_failed"`
	WaitSeedPending     int64 `db:"wait_seed_pending"`
	WaitSeedRunning     int64 `db:"wait_seed_running"`
	WaitSeedFailed      int64 `db:"wait_seed_failed"`
	PoRepPending        int64 `db:"porep_pending"`
	PoRepRunning        int64 `db:"porep_running"`
	PoRepFailed         int64 `db:"porep_failed"`
	CommitMsgPending    int64 `db:"commit_msg_pending"`
	CommitMsgRunning    int64 `db:"commit_msg_running"`
	CommitMsgFailed     int64 `db:"commit_msg_failed"`
	EncodeRunning       int64 `db:"encode_running"`
	EncodePending       int64 `db:"encode_pending"`
	EncodeFailed        int64 `db:"encode_failed"`
	ProveRunning        int64 `db:"prove_running"`
	ProvePending        int64 `db:"prove_pending"`
	ProveFailed         int64 `db:"prove_failed"`
	SubmitRunning       int64 `db:"submit_running"`
	SubmitPending       int64 `db:"submit_pending"`
	SubmitFailed        int64 `db:"submit_failed"`
	MoveStorageRunning  int64 `db:"move_storage_running"`
	MoveStoragePending  int64 `db:"move_storage_pending"`
	MoveStorageFailed   int64 `db:"move_storage_failed"`
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

const cidGravityDealCheckUrl = "https://api.cidgravity.com/api/proposal/check"
const cidGravityMinerCheckUrl = "https://service.cidgravity.com/private/v1/miner-status-checker/check"
const cidGravityMinerCheckLabel = "cidg-miner-status-check"
const agentName = "curio"

func (m *MK12) cidGravityCheck(ctx context.Context, deal *ProviderDealState) (bool, string, error) {

	data, err := m.prepareCidGravityPayload(ctx, deal)
	if err != nil {
		return false, "", xerrors.Errorf("Error preparing cid gravity payload: %w", err)
	}

	// Creating a new HTTP client
	client := &http.Client{}

	// Create the new request
	req, err := http.NewRequest("POST", cidGravityDealCheckUrl, bytes.NewBuffer(data))
	if err != nil {
		return false, "", xerrors.Errorf("Error creating request: %w", err)
	}

	var isCidGravityMinerCheckDeal bool

	if deal.ClientDealProposal.Proposal.Label.IsString() {
		lableStr, err := deal.ClientDealProposal.Proposal.Label.ToString()
		if err != nil {
			return false, "", xerrors.Errorf("Error getting label string: %w", err)
		}
		if strings.HasPrefix(lableStr, cidGravityMinerCheckLabel) {
			req, err = http.NewRequest("POST", cidGravityMinerCheckUrl, bytes.NewBuffer(data))
			if err != nil {
				return false, "", xerrors.Errorf("Error creating request: %w", err)
			}
			isCidGravityMinerCheckDeal = true
		}
	}

	// Add necessary headers
	req.Header = http.Header{
		"X-CIDgravity-Agent":   []string{"CIDgravity-storage-Connector"},
		"X-CIDgravity-Version": []string{"1.0"},
		"Content-Type":         []string{"application/json"},
	}

	token, ok := m.cidGravity[deal.ClientDealProposal.Proposal.Provider]
	if !ok {
		return false, "", xerrors.Errorf("No cid gravity token for provider %s", deal.ClientDealProposal.Proposal.Provider)
	}

	req.Header.Set("X-API-KEY", token)
	req.Header.Set("X-Address-ID", deal.ClientDealProposal.Proposal.Provider.String())
	req.Header.Set("Authorization", token)

	hdr, err := json.Marshal(req.Header)
	if err != nil {
		return false, "", xerrors.Errorf("Error marshaling headers: %w", err)
	}
	log.Debugw("cid gravity request ", "url", req.URL.String(), "headers", string(hdr), "body", string(data))

	// Execute the request
	resp, err := client.Do(req)
	if err != nil {
		return false, "", xerrors.Errorf("Error making request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Errorf("Error closing response body: %w", err)
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
		return false, "", xerrors.Errorf("Error reading response body: %w", err)
	}

	response := cidGravityResponse{}
	err = json.Unmarshal(body.Bytes(), &response)

	if err != nil {
		return false, "", xerrors.Errorf("Error parsing response body: %w", err)
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
		if isCidGravityMinerCheckDeal {
			return false, "Rejected the CID Gravity Check Deal", nil
		}
		return true, "", nil
	}

	return false, fmt.Sprintf("%s. %s", response.ExternalMessage, response.CustomMessage), nil
}

func (m *MK12) prepareCidGravityPayload(ctx context.Context, deal *ProviderDealState) ([]byte, error) {

	head, err := m.api.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting chain head: %w", err)
	}

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
	data.SkipIPNIAnnounce = !deal.AnnounceToIPNI
	data.RemoveUnsealedCopy = !deal.FastRetrieval

	data.DealTransferType = deal.Transfer.Type
	data.ClientDealProposal.ClientSignature.Data = deal.ClientDealProposal.ClientSignature.Data
	data.ClientDealProposal.ClientSignature.Type = deal.ClientDealProposal.ClientSignature.Type
	data.ClientDealProposal.Proposal = deal.ClientDealProposal.Proposal
	data.DealDataRoot.CID = deal.DealDataRoot.String()

	// Fund details
	mbal, err := m.api.StateMarketBalance(ctx, deal.ClientDealProposal.Proposal.Provider, head.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting provider market balance: %w", err)
	}

	data.FundsState.Escrow.Tagged = big.NewInt(0)
	data.FundsState.Escrow.Locked = mbal.Locked
	data.FundsState.Escrow.Available = big.Sub(mbal.Escrow, mbal.Locked)

	data.FundsState.Collateral.Address = deal.ClientDealProposal.Proposal.Provider
	data.FundsState.Collateral.Balance = big.Sub(mbal.Escrow, mbal.Locked)

	mi, err := m.api.StateMinerInfo(ctx, deal.ClientDealProposal.Proposal.Provider, head.Key())
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

	err = m.db.Select(ctx, &stats, `SELECT COALESCE(SUM(available), 0) as available, COALESCE(SUM(capacity), 0) as capacity FROM storage_path WHERE can_seal = true`)
	if err != nil {
		return nil, xerrors.Errorf("failed to run storage query: %w", err)
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

	err = m.db.Select(ctx, &pipelineStats, `SELECT COUNT(*) FILTER (WHERE started = FALSE AND offline = TRUE) AS not_started_offline,
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
													   mt.owner_id AS move_storage_owner,
											
													   -- Detect missing task entries (use CASE WHEN instead of direct boolean expression)
													   CASE WHEN (sp.task_id_encode IS NOT NULL AND et.id IS NULL) THEN TRUE ELSE FALSE END AS encode_missing,
													   CASE WHEN (sp.task_id_prove IS NOT NULL AND pt.id IS NULL) THEN TRUE ELSE FALSE END AS prove_missing,
													   CASE WHEN (sp.task_id_submit IS NOT NULL AND st.id IS NULL) THEN TRUE ELSE FALSE END AS submit_missing,
													   CASE WHEN (sp.task_id_move_storage IS NOT NULL AND mt.id IS NULL) THEN TRUE ELSE FALSE END AS move_storage_missing
											
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
												COUNT(*) FILTER (WHERE after_submit = true AND after_move_storage = false AND task_id_move_storage IS NOT NULL AND move_storage_owner IS NOT NULL) AS move_storage_running,
											
												-- Failure counts
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

		ct := cts[0]

		data.SealingPipelineState.Pipeline.States = structToNestedMap(ct)

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
												cmt.owner_id AS commit_msg_owner,
										
												-- Detect missing task entries
												CASE WHEN (sp.task_id_sdr IS NOT NULL AND sdrt.id IS NULL) THEN TRUE ELSE FALSE END AS sdr_missing,
												CASE 
													WHEN (sp.task_id_tree_d IS NOT NULL AND tdt.id IS NULL)
													  OR (sp.task_id_tree_c IS NOT NULL AND tct.id IS NULL)
													  OR (sp.task_id_tree_r IS NOT NULL AND trt.id IS NULL)
													THEN TRUE ELSE FALSE
												END AS trees_missing,
												CASE WHEN (sp.task_id_precommit_msg IS NOT NULL AND pmt.id IS NULL) THEN TRUE ELSE FALSE END AS precommit_msg_missing,
												CASE WHEN (sp.task_id_porep IS NOT NULL AND pot.id IS NULL) THEN TRUE ELSE FALSE END AS porep_missing,
												CASE WHEN (sp.task_id_commit_msg IS NOT NULL AND cmt.id IS NULL) THEN TRUE ELSE FALSE END AS commit_msg_missing
										
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
												(after_sdr = FALSE) AS at_sdr,
												(after_sdr = TRUE AND (after_tree_d = FALSE OR after_tree_c = FALSE OR after_tree_r = FALSE)) AS at_trees,
												(after_tree_r = TRUE AND after_precommit_msg = FALSE) AS at_precommit_msg,
												(after_precommit_msg_success = TRUE AND seed_epoch > $1) AS at_wait_seed,
												(after_porep = FALSE AND after_precommit_msg_success = TRUE AND seed_epoch < $1) AS at_porep,
												(after_commit_msg_success = FALSE AND after_porep = TRUE) AS at_commit_msg,
												(after_commit_msg_success = TRUE) AS at_done,
												(failed = TRUE) AS at_failed
											FROM pipeline_data
										)
										SELECT
											-- SDR Stage
											COUNT(*) FILTER (WHERE (at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NULL)) AS sdr_pending,
											COUNT(*) FILTER (WHERE (at_sdr AND task_id_sdr IS NOT NULL AND sdr_owner IS NOT NULL)) AS sdr_running,
										
											-- Trees Stage
											COUNT(*) FILTER (
												WHERE at_trees
												  AND (
													  (task_id_tree_d IS NOT NULL AND tree_d_owner IS NULL AND after_tree_d = FALSE)
												   OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NULL AND after_tree_c = FALSE)
												   OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NULL AND after_tree_r = FALSE)
												  )
											) AS trees_pending,
											COUNT(*) FILTER (
												WHERE at_trees
												  AND (
													  (task_id_tree_d IS NOT NULL AND tree_d_owner IS NOT NULL AND after_tree_d = FALSE)
												   OR (task_id_tree_c IS NOT NULL AND tree_c_owner IS NOT NULL AND after_tree_c = FALSE)
												   OR (task_id_tree_r IS NOT NULL AND tree_r_owner IS NOT NULL AND after_tree_r = FALSE)
												  )
											) AS trees_running,
										
											-- PrecommitMsg Stage
											COUNT(*) FILTER (WHERE (at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NULL)) AS precommit_msg_pending,
											COUNT(*) FILTER (WHERE (at_precommit_msg AND task_id_precommit_msg IS NOT NULL AND precommit_msg_owner IS NOT NULL)) AS precommit_msg_running,
										
											-- WaitSeed Stage
											0 AS wait_seed_pending,
											COUNT(*) FILTER (WHERE (at_wait_seed)) AS wait_seed_running,
											0 AS wait_seed_failed,
										
											-- PoRep Stage
											COUNT(*) FILTER (WHERE (at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NULL)) AS porep_pending,
											COUNT(*) FILTER (WHERE (at_porep AND task_id_porep IS NOT NULL AND porep_owner IS NOT NULL)) AS porep_running,
										
											-- CommitMsg Stage
											COUNT(*) FILTER (WHERE (at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NULL)) AS commit_msg_pending,
											COUNT(*) FILTER (WHERE (at_commit_msg AND task_id_commit_msg IS NOT NULL AND commit_msg_owner IS NOT NULL)) AS commit_msg_running,
										
											-- Failure Counts
											COUNT(*) FILTER (WHERE sdr_missing) AS sdr_failed,
											COUNT(*) FILTER (WHERE trees_missing) AS trees_failed,
											COUNT(*) FILTER (WHERE precommit_msg_missing) AS precommit_msg_failed,
											COUNT(*) FILTER (WHERE porep_missing) AS porep_failed,
											COUNT(*) FILTER (WHERE commit_msg_missing) AS commit_msg_failed
										
										FROM stages`, head.Height())
		if err != nil {
			return nil, xerrors.Errorf("failed to run sdr pipeline stage query: %w", err)
		}

		if len(cts) == 0 {
			return nil, xerrors.Errorf("expected 1 row, got 0")
		}

		ct := cts[0]

		data.SealingPipelineState.Pipeline.States = structToNestedMap(ct)
	}

	// Apply backpressure
	wait, err := m.maybeApplyBackpressure(ctx, deal.ClientDealProposal.Proposal.Provider)
	if err != nil {
		return nil, xerrors.Errorf("applying backpressure: %w", err)
	}

	data.SealingPipelineState.Pipeline.UnderBackPressure = wait

	return json.Marshal(data)
}

func structToNestedMap(input interface{}) map[string]map[string]int64 {
	result := map[string]map[string]int64{
		"Running": {},
		"Pending": {},
		"Failed":  {},
	}

	v := reflect.ValueOf(input)
	t := v.Type()

	// Regular expression to extract category names (e.g., "SDR", "Prove", "Submit", "MoveStorage")
	re := regexp.MustCompile(`(.*?)(Pending|Running|Failed)$`)

	for i := 0; i < v.NumField(); i++ {
		fieldName := t.Field(i).Name
		fieldValue := v.Field(i).Interface().(int64) // Cast to int64

		// Extract base name and category
		matches := re.FindStringSubmatch(fieldName)
		if len(matches) != 3 {
			continue // Skip if the name doesn't match expected pattern
		}
		category := matches[1] // Extract base category name (e.g., "SDR", "Prove")
		state := matches[2]    // Extract state (e.g., "Running", "Pending", "Failed")

		// Ensure category key exists in result
		if _, exists := result[state]; !exists {
			result[state] = make(map[string]int64)
		}
		result[state][category] = fieldValue
	}

	return result
}
