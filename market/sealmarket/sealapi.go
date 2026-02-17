package sealmarket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/tarutil"
)

var log = logging.Logger("sealmarket")

// slotEntry is an in-memory slot reservation with a deadline.
type slotEntry struct {
	partnerID int64
	deadline  time.Time
}

type SealMarket struct {
	db *harmonydb.DB
	sc *ffi2.SealCalls

	slotsMu sync.Mutex
	slots   map[string]*slotEntry
}

func NewSealMarket(db *harmonydb.DB, sc *ffi2.SealCalls) *SealMarket {
	return &SealMarket{
		db:    db,
		sc:    sc,
		slots: make(map[string]*slotEntry),
	}
}

const SealMarketRoutePath = "/remoteseal/"
const DelegatedSealPath = SealMarketRoutePath + "delegated/v0/"

/*
/remoteseal/delegated/v0 -> v0 trusted delegated sealing

// setup flow
1. Client provides partner_url (base cuhttp url)
2. Provider in UI sets the URL, partner_name, total allowance
3. Providers curio contacts partner_url to check remoteseal capabilities
4. Provider UI returns a connect-string (token+cuhttp endpoint b64 encoded)
5. Client sets connect-string in UI, and clicks "connect"
6. Client curio -> provider /remoteseal/delegated/v0/capabilities
7. Client curio -> provider /remoteseal/delegated/v0/authorize (stateless auth confirm)
8. Client curio adds provider to DB

--
// sealing flow

1. Client starts cc sector as usual, e.g with cc scheduler
2. Sealing pipeline creates sectors_sdr_pipeline entry
3. RSealDelegate task picks up the task, similar to supra batch seal task, creates rseal_client_pipeline entry
  3.1. IFF we have providers that are available
  3.2. With 5s timeout contact provider to check /available -> true/false + 30s available slot token
4. If provider is available, rseal_client_pipeline entry is created
5. RSealDelegate task starts client side, sends /remoteseal/delegated/v0/order to provider with sector details
6. Provider spawns matching pipeline
7. Provider SDR task computes ticket from chain locally and runs SDR
8. Trees run and finish provider side
9. Provider sends /remoteseal/delegated/v0/complete to client (includes ticket), client also polls /remoteseal/delegated/v0/status
10.1. Client sends precommit through the normal precommit pipeline
10.2. Client fetches sealed file: GET /remoteseal/delegated/v0/sealed-data/{sp_id}/{sector_number}?token=... (32 GiB, Range, aria2c)
10.3. Client fetches fincache: GET /remoteseal/delegated/v0/cache-data/{sp_id}/{sector_number}?token=... (tar, ~73 MiB)
11. At C1 client contacts provider /remoteseal/delegated/v0/commit1 to supply C1 seed and get C1 output
12. After client records C1 output, client sends /remoteseal/delegated/v0/finalize to provider letting the provider know that layers can be dropped
13. After all data is client-side, client sends /remoteseal/delegated/v0/cleanup to provider letting the provider know that cleanup can begin
*/

// --- Request/Response types ---

// CapabilitiesResponse is returned by the provider to describe what it can do.
type CapabilitiesResponse struct {
	// SupportedProofs lists the registered seal proof types this provider supports.
	SupportedProofs []int64 `json:"supported_proofs"`

	// MaxBatchSize is the maximum batch size the provider can handle (e.g. 128 for supraseal).
	MaxBatchSize int `json:"max_batch_size"`

	// SupportsRangeRequests indicates sealed-data endpoint supports HTTP Range headers.
	SupportsRangeRequests bool `json:"supports_range_requests"`
}

// AuthorizeRequest is sent by the client to confirm auth works.
type AuthorizeRequest struct {
	PartnerToken string `json:"partner_token"`
}

// AuthorizeResponse confirms the partner is recognized and has allowance.
type AuthorizeResponse struct {
	Authorized         bool   `json:"authorized"`
	PartnerName        string `json:"partner_name"`
	AllowanceRemaining int64  `json:"allowance_remaining"`
}

// AvailableResponse is returned when checking provider slot availability.
type AvailableResponse struct {
	Available bool   `json:"available"`
	SlotToken string `json:"slot_token,omitempty"` // 30s reservation token
}

// OrderRequest is sent by the client to create a remote seal order.
// Delegated sectors are always CC (no deal data). CommD is the static
// zero-commitment derived from the sector size (reg_seal_proof).
type OrderRequest struct {
	PartnerToken string `json:"partner_token"`
	SlotToken    string `json:"slot_token"` // from /available

	SpID         int64 `json:"sp_id"`
	SectorNumber int64 `json:"sector_number"`
	RegSealProof int   `json:"reg_seal_proof"`
}

// OrderResponse confirms the order was accepted by the provider.
// The provider seals under the client's sp_id/sector_number (no separate provider identity).
type OrderResponse struct {
	Accepted     bool   `json:"accepted"`
	RejectReason string `json:"reject_reason,omitempty"`
}

// StatusRequest is used by the client to poll completion status.
type StatusRequest struct {
	PartnerToken string `json:"partner_token"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
}

// StatusResponse describes the current state of a remote seal job.
type StatusResponse struct {
	State       string `json:"state"` // "pending", "sdr", "trees", "complete", "failed"
	TreeDCid    string `json:"tree_d_cid,omitempty"`
	TreeRCid    string `json:"tree_r_cid,omitempty"`
	TicketEpoch int64  `json:"ticket_epoch,omitempty"`
	TicketValue []byte `json:"ticket_value,omitempty"`
	FailReason  string `json:"fail_reason,omitempty"`
}

// CompleteNotification is sent by the provider to the client when SDR+trees finish.
type CompleteNotification struct {
	PartnerToken string `json:"partner_token"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
	TreeDCid     string `json:"tree_d_cid"`
	TreeRCid     string `json:"tree_r_cid"`
	TicketEpoch  int64  `json:"ticket_epoch"`
	TicketValue  []byte `json:"ticket_value"`
}

// Commit1Request is sent by the client to the provider to exchange C1 seed for C1 output.
type Commit1Request struct {
	PartnerToken string `json:"partner_token"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
	SeedEpoch    int64  `json:"seed_epoch"`
	SeedValue    []byte `json:"seed_value"`
}

// FinalizeRequest is sent by the client to tell the provider layers can be dropped.
type FinalizeRequest struct {
	PartnerToken string `json:"partner_token"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
}

// CleanupRequest is sent by the client to tell the provider to begin full cleanup.
type CleanupRequest struct {
	PartnerToken string `json:"partner_token"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
}

// --- Routes ---

func Routes(r *chi.Mux, sm *SealMarket) {
	r.Route(DelegatedSealPath, func(r chi.Router) {
		r.Use(rsealMetricsMiddleware)

		// Setup flow endpoints (called by client)
		r.Get("/capabilities", sm.handleCapabilities)
		r.Post("/authorize", sm.handleAuthorize)

		// Availability check (called by client)
		r.Post("/available", sm.handleAvailable)

		// Sealing flow - provider-side endpoints (called by client)
		r.Post("/order", sm.handleOrder)
		r.Post("/status", sm.handleStatus)
		r.Get("/sealed-data/{sp_id}/{sector_number}", sm.handleSealedData) // serves sealed file; supports Range header
		r.Get("/cache-data/{sp_id}/{sector_number}", sm.handleCacheData)   // serves fincache tar (p_aux, t_aux, tree-r-last)
		r.Post("/commit1", sm.handleCommit1)
		r.Post("/finalize", sm.handleFinalize)
		r.Post("/cleanup", sm.handleCleanup)

		// Sealing flow - client-side endpoints (called by provider)
		r.Post("/complete", sm.handleComplete)
	})
}

// --- Handler implementations ---

// handleCapabilities returns what this provider supports.
// GET /remoteseal/delegated/v0/capabilities
func (sm *SealMarket) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	resp := CapabilitiesResponse{
		SupportedProofs: []int64{
			int64(abi.RegisteredSealProof_StackedDrg32GiBV1_1),
			int64(abi.RegisteredSealProof_StackedDrg64GiBV1_1),
			int64(abi.RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep),
			int64(abi.RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep),
		},
		MaxBatchSize:          128,
		SupportsRangeRequests: true,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAuthorize confirms the partner token is valid and has allowance.
// POST /remoteseal/delegated/v0/authorize
func (sm *SealMarket) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	var req AuthorizeRequest
	if !readJSON(w, r, &req) {
		return
	}

	var partners []struct {
		PartnerName        string `db:"partner_name"`
		AllowanceRemaining int64  `db:"allowance_remaining"`
	}

	err := sm.db.Select(r.Context(), &partners, `SELECT partner_name, allowance_remaining FROM rseal_delegated_partners WHERE partner_token = $1`, req.PartnerToken)
	if err != nil {
		log.Errorw("authorize: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(partners) == 0 {
		writeJSON(w, http.StatusOK, AuthorizeResponse{Authorized: false})
		return
	}

	resp := AuthorizeResponse{
		Authorized:         true,
		PartnerName:        partners[0].PartnerName,
		AllowanceRemaining: partners[0].AllowanceRemaining,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAvailable checks if the provider has capacity for a new seal job.
// POST /remoteseal/delegated/v0/available
func (sm *SealMarket) handleAvailable(w http.ResponseWriter, r *http.Request) {
	var req AuthorizeRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Look up partner
	var partners []struct {
		ID             int64 `db:"id"`
		AllowanceTotal int64 `db:"allowance_total"`
	}

	err := sm.db.Select(r.Context(), &partners, `SELECT id, allowance_total FROM rseal_delegated_partners WHERE partner_token = $1`, req.PartnerToken)
	if err != nil {
		log.Errorw("available: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(partners) == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	partner := partners[0]

	// Count active (non-cleaned-up) sectors for this partner
	var counts []struct {
		Count int64 `db:"count"`
	}

	err = sm.db.Select(r.Context(), &counts, `SELECT COUNT(*) AS count FROM rseal_provider_pipeline WHERE partner_id = $1 AND after_cleanup != TRUE`, partner.ID)
	if err != nil {
		log.Errorw("available: count query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	activeCount := int64(0)
	if len(counts) > 0 {
		activeCount = counts[0].Count
	}

	if activeCount >= partner.AllowanceTotal {
		writeJSON(w, http.StatusOK, AvailableResponse{Available: false})
		return
	}

	// Generate a slot token with 30s expiry
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		log.Errorw("available: failed to generate slot token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	slotToken := hex.EncodeToString(tokenBytes)

	sm.slotsMu.Lock()
	sm.slots[slotToken] = &slotEntry{
		partnerID: partner.ID,
		deadline:  time.Now().Add(30 * time.Second),
	}
	sm.slotsMu.Unlock()

	stats.Record(r.Context(), RsealSlotsIssued.M(1))

	writeJSON(w, http.StatusOK, AvailableResponse{
		Available: true,
		SlotToken: slotToken,
	})
}

// handleOrder accepts a remote seal order from a client.
// Idempotent: re-submitting the same (sp_id, sector_number) returns the existing order.
// POST /remoteseal/delegated/v0/order
func (sm *SealMarket) handleOrder(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), req.PartnerToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Check allowed proof types for this partner
	{
		var partners []struct {
			AllowedProofTypes []int64 `db:"allowed_proof_types"`
		}
		if err := sm.db.Select(r.Context(), &partners, `SELECT allowed_proof_types FROM rseal_delegated_partners WHERE id = $1`, partnerID); err != nil {
			log.Errorw("order: failed to query allowed proof types", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if len(partners) > 0 && len(partners[0].AllowedProofTypes) > 0 {
			allowed := false
			for _, pt := range partners[0].AllowedProofTypes {
				if pt == int64(req.RegSealProof) {
					allowed = true
					break
				}
			}
			if !allowed {
				writeJSON(w, http.StatusOK, OrderResponse{
					Accepted:     false,
					RejectReason: fmt.Sprintf("proof type %d not allowed for this partner", req.RegSealProof),
				})
				return
			}
		}
	}

	// Validate slot token (if present)
	if req.SlotToken != "" {
		sm.slotsMu.Lock()
		entry, ok := sm.slots[req.SlotToken]
		if ok {
			if time.Now().After(entry.deadline) || entry.partnerID != partnerID {
				ok = false
			}
			delete(sm.slots, req.SlotToken)
		}
		sm.slotsMu.Unlock()

		if !ok {
			_ = stats.RecordWithTags(r.Context(), []tag.Mutator{
				tag.Upsert(RsealAcceptedKey, "false"),
			}, RsealOrdersTotal.M(1))

			writeJSON(w, http.StatusOK, OrderResponse{
				Accepted:     false,
				RejectReason: "invalid or expired slot token",
			})
			return
		}
	}

	// Insert the pipeline entry (idempotent via ON CONFLICT DO NOTHING)
	n, err := sm.db.Exec(r.Context(), `INSERT INTO rseal_provider_pipeline (partner_id, sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3, $4) ON CONFLICT (sp_id, sector_number) DO NOTHING`,
		partnerID, req.SpID, req.SectorNumber, req.RegSealProof)
	if err != nil {
		log.Errorw("order: insert failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// If a new row was inserted, decrement the allowance
	if n == 1 {
		_, err = sm.db.Exec(r.Context(), `UPDATE rseal_delegated_partners SET allowance_remaining = allowance_remaining - 1 WHERE id = $1 AND allowance_remaining > 0`, partnerID)
		if err != nil {
			log.Errorw("order: decrement allowance failed", "error", err)
			// The pipeline entry was created, so we still return accepted
		}
	}

	_ = stats.RecordWithTags(r.Context(), []tag.Mutator{
		tag.Upsert(RsealAcceptedKey, "true"),
	}, RsealOrdersTotal.M(1))

	writeJSON(w, http.StatusOK, OrderResponse{Accepted: true})
}

// handleStatus returns the current state of a remote seal job.
// POST /remoteseal/delegated/v0/status
func (sm *SealMarket) handleStatus(w http.ResponseWriter, r *http.Request) {
	var req StatusRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), req.PartnerToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var rows []struct {
		TaskIDSdr       *int64  `db:"task_id_sdr"`
		TicketEpoch     *int64  `db:"ticket_epoch"`
		TicketValue     []byte  `db:"ticket_value"`
		AfterSDR        bool    `db:"after_sdr"`
		AfterTreeC      bool    `db:"after_tree_c"`
		AfterTreeR      bool    `db:"after_tree_r"`
		TreeDCid        *string `db:"tree_d_cid"`
		TreeRCid        *string `db:"tree_r_cid"`
		Failed          bool    `db:"failed"`
		FailedReasonMsg string  `db:"failed_reason_msg"`
	}

	err = sm.db.Select(r.Context(), &rows, `SELECT task_id_sdr, ticket_epoch, ticket_value, after_sdr, after_tree_c, after_tree_r, tree_d_cid, tree_r_cid, failed, failed_reason_msg FROM rseal_provider_pipeline WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`,
		req.SpID, req.SectorNumber, partnerID)
	if err != nil {
		log.Errorw("status: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(rows) == 0 {
		http.Error(w, "sector not found", http.StatusNotFound)
		return
	}

	row := rows[0]
	resp := StatusResponse{}

	if row.Failed {
		resp.State = "failed"
		resp.FailReason = row.FailedReasonMsg
	} else if row.AfterTreeR && row.AfterTreeC {
		resp.State = "complete"
		if row.TreeDCid != nil {
			resp.TreeDCid = *row.TreeDCid
		}
		if row.TreeRCid != nil {
			resp.TreeRCid = *row.TreeRCid
		}
		if row.TicketEpoch != nil {
			resp.TicketEpoch = *row.TicketEpoch
			resp.TicketValue = row.TicketValue
		}
	} else if row.AfterSDR {
		resp.State = "trees"
	} else if row.TaskIDSdr != nil {
		resp.State = "sdr"
	} else {
		resp.State = "pending"
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleComplete is called by the provider to notify the client that SDR+trees are done.
// Idempotent: if already marked complete, returns 200 OK without error.
// POST /remoteseal/delegated/v0/complete
func (sm *SealMarket) handleComplete(w http.ResponseWriter, r *http.Request) {
	var req CompleteNotification
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token by looking up in rseal_client_providers
	var providers []struct {
		ID int64 `db:"id"`
	}

	err := sm.db.Select(r.Context(), &providers, `SELECT id FROM rseal_client_providers WHERE provider_token = $1`, req.PartnerToken)
	if err != nil {
		log.Errorw("complete: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(providers) == 0 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	providerID := providers[0].ID

	// Check if already complete
	var existing []struct {
		AfterSDR bool `db:"after_sdr"`
	}

	err = sm.db.Select(r.Context(), &existing, `SELECT after_sdr FROM rseal_client_pipeline WHERE sp_id = $1 AND sector_number = $2 AND provider_id = $3`, req.SpID, req.SectorNumber, providerID)
	if err != nil {
		log.Errorw("complete: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(existing) == 0 {
		http.Error(w, "sector not found", http.StatusNotFound)
		return
	}

	// Already complete - idempotent
	if existing[0].AfterSDR {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Apply the completion using shared function (ticket comes from the provider notification)
	err = ApplyRemoteCompletion(r.Context(), sm.db, req.SpID, req.SectorNumber, providerID,
		req.TreeDCid, req.TreeRCid, req.TicketEpoch, req.TicketValue)
	if err != nil {
		log.Errorw("complete: transaction failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	log.Infow("remote seal complete notification applied",
		"sp_id", req.SpID, "sector", req.SectorNumber,
		"tree_d_cid", req.TreeDCid, "tree_r_cid", req.TreeRCid)

	w.WriteHeader(http.StatusOK)
}

// handleSealedData streams the sealed sector file (32 GiB) to the client.
// Supports HTTP Range headers for resumable downloads (aria2c compatible).
// Auth via ?token= query param (GET can't have body for proper Range support).
// GET /remoteseal/delegated/v0/sealed-data/{sp_id}/{sector_number}?token=...
func (sm *SealMarket) handleSealedData(w http.ResponseWriter, r *http.Request) {
	spID, sectorNumber, ok := parseSectorPathParams(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Look up the sector in rseal_provider_pipeline to get the proof type
	var sectors []struct {
		RegSealProof int `db:"reg_seal_proof"`
	}

	err = sm.db.Select(r.Context(), &sectors, `SELECT reg_seal_proof FROM rseal_provider_pipeline WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`, spID, sectorNumber, partnerID)
	if err != nil {
		log.Errorw("sealed-data: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(sectors) == 0 {
		http.Error(w, "sector not found", http.StatusNotFound)
		return
	}

	// Build SectorRef
	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(spID),
			Number: abi.SectorNumber(sectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectors[0].RegSealProof),
	}

	// Acquire the sealed file path
	paths, _, release, err := sm.sc.Sectors.AcquireSector(r.Context(), nil, sref, storiface.FTSealed, storiface.FTNone, storiface.PathStorage)
	if err != nil {
		log.Errorw("sealed-data: acquire sector failed", "error", err)
		http.Error(w, "sector data not available", http.StatusInternalServerError)
		return
	}
	defer release()

	if paths.Sealed == "" {
		http.Error(w, "sealed file not found", http.StatusNotFound)
		return
	}

	// http.ServeFile handles Range headers automatically
	http.ServeFile(w, r, paths.Sealed)
}

// handleCacheData streams the finalized cache (p_aux, t_aux, tree-r-last) as a tar archive.
// Uses FinCacheFileConstraints from tarutil (~73 MiB total).
// Auth via ?token= query param.
// GET /remoteseal/delegated/v0/cache-data/{sp_id}/{sector_number}?token=...
func (sm *SealMarket) handleCacheData(w http.ResponseWriter, r *http.Request) {
	spID, sectorNumber, ok := parseSectorPathParams(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Look up the sector in rseal_provider_pipeline to get the proof type
	var sectors []struct {
		RegSealProof int `db:"reg_seal_proof"`
	}

	err = sm.db.Select(r.Context(), &sectors, `SELECT reg_seal_proof FROM rseal_provider_pipeline WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`, spID, sectorNumber, partnerID)
	if err != nil {
		log.Errorw("cache-data: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(sectors) == 0 {
		http.Error(w, "sector not found", http.StatusNotFound)
		return
	}

	// Build SectorRef
	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(spID),
			Number: abi.SectorNumber(sectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sectors[0].RegSealProof),
	}

	// Acquire the cache path
	paths, _, release, err := sm.sc.Sectors.AcquireSector(r.Context(), nil, sref, storiface.FTCache, storiface.FTNone, storiface.PathStorage)
	if err != nil {
		log.Errorw("cache-data: acquire sector failed", "error", err)
		http.Error(w, "sector data not available", http.StatusInternalServerError)
		return
	}
	defer release()

	if paths.Cache == "" {
		http.Error(w, "cache directory not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/x-tar")
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 1<<20) // 1 MiB buffer
	if err := tarutil.TarDirectory(tarutil.FinCacheFileConstraints, paths.Cache, w, buf); err != nil {
		log.Errorw("cache-data: tar write failed", "error", err)
		// Cannot send HTTP error at this point since we already wrote the header
		return
	}
}

// handleCommit1 accepts the C1 seed from the client and returns C1 output.
// POST /remoteseal/delegated/v0/commit1
func (sm *SealMarket) handleCommit1(w http.ResponseWriter, r *http.Request) {
	var req Commit1Request
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), req.PartnerToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Look up sector in rseal_provider_pipeline
	var sectors []struct {
		RegSealProof int     `db:"reg_seal_proof"`
		TicketEpoch  *int64  `db:"ticket_epoch"`
		TicketValue  []byte  `db:"ticket_value"`
		TreeDCid     *string `db:"tree_d_cid"`
		TreeRCid     *string `db:"tree_r_cid"`
		AfterTreeR   bool    `db:"after_tree_r"`
	}

	err = sm.db.Select(r.Context(), &sectors, `SELECT reg_seal_proof, ticket_epoch, ticket_value, tree_d_cid, tree_r_cid, after_tree_r FROM rseal_provider_pipeline WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`,
		req.SpID, req.SectorNumber, partnerID)
	if err != nil {
		log.Errorw("commit1: db query failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(sectors) == 0 {
		http.Error(w, "sector not found", http.StatusNotFound)
		return
	}

	sector := sectors[0]

	if !sector.AfterTreeR {
		http.Error(w, "sector trees not yet complete", http.StatusPreconditionFailed)
		return
	}

	if sector.TreeDCid == nil || sector.TreeRCid == nil {
		http.Error(w, "sector CIDs not available", http.StatusPreconditionFailed)
		return
	}

	// Parse CIDs
	sealedCID, err := cid.Decode(*sector.TreeRCid)
	if err != nil {
		log.Errorw("commit1: invalid tree_r_cid", "error", err, "cid", *sector.TreeRCid)
		http.Error(w, "invalid tree_r_cid", http.StatusInternalServerError)
		return
	}

	unsealedCID, err := cid.Decode(*sector.TreeDCid)
	if err != nil {
		log.Errorw("commit1: invalid tree_d_cid", "error", err, "cid", *sector.TreeDCid)
		http.Error(w, "invalid tree_d_cid", http.StatusInternalServerError)
		return
	}

	// Build SectorRef
	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(req.SpID),
			Number: abi.SectorNumber(req.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sector.RegSealProof),
	}

	// Ensure synthetic proofs exist. The provider runs SDR+trees but not the
	// normal Synth task (which also clears layers and generates the unsealed
	// copy). SealCommitPhase1 requires syn-porep-vanilla-proofs.dat for
	// synthetic proof types. Generate it now if it doesn't already exist.
	ssize, err := sref.ProofType.SectorSize()
	if err != nil {
		log.Errorw("commit1: get sector size", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Remote-sealed sectors are always CC: single piece with size = sector size, PieceCID = unsealedCID (zero-comm)
	pieces := []abi.PieceInfo{{
		Size:     abi.PaddedPieceSize(ssize),
		PieceCID: unsealedCID,
	}}

	computeStart := time.Now()

	if err := sm.sc.EnsureSyntheticProofs(r.Context(), sref, sealedCID, unsealedCID, abi.SealRandomness(sector.TicketValue), pieces); err != nil {
		log.Errorw("commit1: EnsureSyntheticProofs failed", "error", err)
		http.Error(w, "failed to generate synthetic proofs", http.StatusInternalServerError)
		return
	}

	// Compute the vanilla proof (C1)
	vanillaProof, err := sm.sc.GeneratePoRepVanillaProof(
		r.Context(),
		sref,
		sealedCID,
		unsealedCID,
		abi.SealRandomness(sector.TicketValue),
		abi.InteractiveSealRandomness(req.SeedValue),
	)
	if err != nil {
		log.Errorw("commit1: GeneratePoRepVanillaProof failed", "error", err)
		http.Error(w, "failed to compute C1", http.StatusInternalServerError)
		return
	}

	stats.Record(r.Context(), RsealCommit1ComputeDuration.M(float64(time.Since(computeStart).Milliseconds())))

	// Mark after_c1_supplied = TRUE
	_, err = sm.db.Exec(r.Context(), `UPDATE rseal_provider_pipeline SET after_c1_supplied = TRUE WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`,
		req.SpID, req.SectorNumber, partnerID)
	if err != nil {
		log.Errorw("commit1: failed to update after_c1_supplied", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Write raw bytes — C1 output is large (~50-128 MiB), JSON+base64 adds ~33% overhead
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(vanillaProof); err != nil {
		log.Errorw("commit1: failed to write response", "error", err)
	}
}

// handleFinalize tells the provider that layers can be dropped (sealed data fetched).
// POST /remoteseal/delegated/v0/finalize
func (sm *SealMarket) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req FinalizeRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), req.PartnerToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Mark after_c1_supplied = TRUE if not already set.
	// The finalize task in the provider poller starts when after_c1_supplied is TRUE.
	// If /commit1 was already called, this is a no-op.
	_, err = sm.db.Exec(r.Context(), `UPDATE rseal_provider_pipeline SET after_c1_supplied = TRUE WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3 AND after_c1_supplied = FALSE`,
		req.SpID, req.SectorNumber, partnerID)
	if err != nil {
		log.Errorw("finalize: failed to update", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleCleanup tells the provider to fully clean up all sector data.
// Idempotent: if already cleaned up or cleanup already requested, returns 200 OK.
// POST /remoteseal/delegated/v0/cleanup
func (sm *SealMarket) handleCleanup(w http.ResponseWriter, r *http.Request) {
	var req CleanupRequest
	if !readJSON(w, r, &req) {
		return
	}

	// Validate partner token
	partnerID, err := sm.validatePartnerToken(r.Context(), req.PartnerToken)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Set cleanup_requested = true (no-op if already set)
	_, err = sm.db.Exec(r.Context(), `UPDATE rseal_provider_pipeline SET cleanup_requested = TRUE WHERE sp_id = $1 AND sector_number = $2 AND partner_id = $3`,
		req.SpID, req.SectorNumber, partnerID)
	if err != nil {
		log.Errorw("cleanup: failed to update", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- Shared functions ---

// ApplyRemoteCompletion updates both rseal_client_pipeline and sectors_sdr_pipeline
// when a remote provider completes SDR+trees. This is called by both the poll task
// (in remoteseal package) and the /complete callback handler.
// The ticket data comes from the provider (via notification or status poll).
func ApplyRemoteCompletion(ctx context.Context, db *harmonydb.DB, spID, sectorNumber, providerID int64, treeDCid, treeRCid string, ticketEpoch int64, ticketValue []byte) error {
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update rseal_client_pipeline: mark SDR and all trees as done
		n, err := tx.Exec(`
			UPDATE rseal_client_pipeline
			SET after_sdr = TRUE, after_tree_d = TRUE, after_tree_c = TRUE, after_tree_r = TRUE,
			    tree_d_cid = $3, tree_r_cid = $4,
			    task_id_sdr = NULL, task_id_tree_d = NULL, task_id_tree_c = NULL, task_id_tree_r = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			spID, sectorNumber, treeDCid, treeRCid)
		if err != nil {
			return false, xerrors.Errorf("updating rseal_client_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 rseal_client_pipeline row, updated %d", n)
		}

		// Update sectors_sdr_pipeline: mark SDR, trees, and synth as done.
		// Set after_synth = TRUE because remote-sealed sectors skip the local synth step.
		// Propagate ticket data from the provider so the PoRep task can use it.
		// Clear task_ids so the normal precommit pipeline can proceed.
		n, err = tx.Exec(`
			UPDATE sectors_sdr_pipeline
			SET after_sdr = TRUE, after_tree_d = TRUE, after_tree_c = TRUE, after_tree_r = TRUE,
			    after_synth = TRUE,
			    tree_d_cid = $3, tree_r_cid = $4,
			    ticket_epoch = $5, ticket_value = $6,
			    task_id_sdr = NULL, task_id_tree_d = NULL, task_id_tree_c = NULL, task_id_tree_r = NULL
			WHERE sp_id = $1 AND sector_number = $2`,
			spID, sectorNumber, treeDCid, treeRCid, ticketEpoch, ticketValue)
		if err != nil {
			return false, xerrors.Errorf("updating sectors_sdr_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 sectors_sdr_pipeline row, updated %d", n)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("applying remote completion transaction: %w", err)
	}

	return nil
}

// --- Helpers ---

// validatePartnerToken checks the partner_token against rseal_delegated_partners
// and returns the partner ID if valid.
func (sm *SealMarket) validatePartnerToken(ctx context.Context, token string) (int64, error) {
	var partners []struct {
		ID int64 `db:"id"`
	}

	err := sm.db.Select(ctx, &partners, `SELECT id FROM rseal_delegated_partners WHERE partner_token = $1`, token)
	if err != nil {
		return 0, xerrors.Errorf("querying partner token: %w", err)
	}

	if len(partners) == 0 {
		return 0, xerrors.Errorf("partner token not found")
	}

	return partners[0].ID, nil
}

func parseSectorPathParams(w http.ResponseWriter, r *http.Request) (spID int64, sectorNumber int64, ok bool) {
	spIDStr := chi.URLParam(r, "sp_id")
	sectorNumberStr := chi.URLParam(r, "sector_number")

	spID, err := strconv.ParseInt(spIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid sp_id", http.StatusBadRequest)
		return 0, 0, false
	}

	sectorNumber, err = strconv.ParseInt(sectorNumberStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid sector_number", http.StatusBadRequest)
		return 0, 0, false
	}

	return spID, sectorNumber, true
}

func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return false
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Errorw("failed to write JSON response", "error", err)
	}
}
