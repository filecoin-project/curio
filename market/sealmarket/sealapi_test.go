package sealmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/testutil/dbtest"
)

// sharedDB is initialized once in TestMain and reused by all tests,
// avoiding the ~65s schema-migration cost per test.
var sharedDB *harmonydb.DB

func TestMain(m *testing.M) {
	code := dbtest.StartYugabyte(m)
	// Note: StartYugabyte calls m.Run() internally, so 'code' is the test result.
	// sharedDB cleanup happens inside StartYugabyte's scope (DB is closed on exit).
	os.Exit(code)
}

// initSharedDB lazily creates the shared DB on first use. It is called
// by setupHarness. A sync.Once is not needed because Go tests run
// sequentially by default within a package.
func initSharedDB(t *testing.T) {
	t.Helper()
	if sharedDB != nil {
		return
	}

	envOr := func(env, fallback string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return fallback
	}

	iTestID := harmonydb.ITestNewID()

	db, err := harmonydb.NewFromConfig(harmonydb.Config{
		Hosts:    []string{envOr("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		Database: envOr("CURIO_HARMONYDB_DB", "yugabyte"),
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     envOr("CURIO_HARMONYDB_PORT", "5433"),
		ITestID:  iTestID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sealapi_test: failed to create shared DB: %v\n", err)
		t.Fatalf("failed to create shared DB: %v", err)
	}

	sharedDB = db

	// Clean up only when the entire test binary finishes.
	// We cannot use t.Cleanup because the first test's cleanup
	// would close the pool before subsequent tests run.
	// Instead we rely on the process exiting to close connections.
	// ITestDeleteAll is nice-to-have but the container is torn down anyway.
}

// testHarness bundles a SealMarket, chi router, and DB handle for API tests.
type testHarness struct {
	t      *testing.T
	db     *harmonydb.DB
	sm     *SealMarket
	router *chi.Mux
}

// setupHarness creates a test SealMarket backed by a real YugabyteDB.
// SealCalls (sc) is nil — none of the auth/constraint endpoints need it.
// All tests share one DB connection; each gets a fresh SealMarket (fresh
// in-memory slot map). Tests use unique partner tokens to avoid collisions.
func setupHarness(t *testing.T) *testHarness {
	t.Helper()
	initSharedDB(t)

	sm := NewSealMarket(sharedDB, nil)
	r := chi.NewMux()
	Routes(r, sm)

	return &testHarness{t: t, db: sharedDB, sm: sm, router: r}
}

// seedPartner inserts a partner row and returns its auto-generated id.
func (h *testHarness) seedPartner(token, name string, allowance int64) int64 {
	h.t.Helper()

	var ids []struct {
		ID int64 `db:"id"`
	}
	err := h.db.Select(context.Background(), &ids,
		`INSERT INTO rseal_delegated_partners (partner_token, partner_name, partner_url, allowance_remaining, allowance_total)
		 VALUES ($1, $2, 'http://test', $3, $3) RETURNING id`,
		token, name, allowance)
	require.NoError(h.t, err)
	require.Len(h.t, ids, 1)
	return ids[0].ID
}

// getAllowance reads the current allowance_remaining for a partner.
func (h *testHarness) getAllowance(partnerID int64) int64 {
	h.t.Helper()

	var rows []struct {
		Remaining int64 `db:"allowance_remaining"`
	}
	err := h.db.Select(context.Background(), &rows,
		`SELECT allowance_remaining FROM rseal_delegated_partners WHERE id = $1`, partnerID)
	require.NoError(h.t, err)
	require.Len(h.t, rows, 1)
	return rows[0].Remaining
}

// postJSON sends a POST with JSON body to the router and returns the recorder.
func (h *testHarness) postJSON(path string, body interface{}) *httptest.ResponseRecorder {
	h.t.Helper()

	b, err := json.Marshal(body)
	require.NoError(h.t, err)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// decodeJSON unmarshals a recorder's body into v.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	require.NoError(t, json.NewDecoder(rec.Body).Decode(v))
}

// --- Authorize tests ---

func TestAuthorize_ValidToken(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-valid", "partner-A", 10)

	rec := h.postJSON(DelegatedSealPath+"authorize", AuthorizeRequest{PartnerToken: "tok-valid"})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AuthorizeResponse
	decodeJSON(t, rec, &resp)
	require.True(t, resp.Authorized)
	require.Equal(t, "partner-A", resp.PartnerName)
	require.Equal(t, int64(10), resp.AllowanceRemaining)
}

func TestAuthorize_InvalidToken(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"authorize", AuthorizeRequest{PartnerToken: "tok-bogus"})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AuthorizeResponse
	decodeJSON(t, rec, &resp)
	require.False(t, resp.Authorized)
}

// --- Auth rejection (401) on endpoints that call validatePartnerToken ---

func TestAvailable_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "bad"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOrder_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"order", OrderRequest{PartnerToken: "bad", SlotToken: "x", SpID: 1, SectorNumber: 1, RegSealProof: 8})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStatus_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"status", StatusRequest{PartnerToken: "bad", SpID: 1, SectorNumber: 1})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestFinalize_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"finalize", FinalizeRequest{PartnerToken: "bad", SpID: 1, SectorNumber: 1})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCleanup_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"cleanup", CleanupRequest{PartnerToken: "bad", SpID: 1, SectorNumber: 1})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCommit1_InvalidToken_401(t *testing.T) {
	h := setupHarness(t)

	rec := h.postJSON(DelegatedSealPath+"commit1", Commit1Request{PartnerToken: "bad", SpID: 1, SectorNumber: 1})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// --- Available / quota tests ---

func TestAvailable_WithCapacity(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-cap", "partner-B", 5)

	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-cap"})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AvailableResponse
	decodeJSON(t, rec, &resp)
	require.True(t, resp.Available)
	require.NotEmpty(t, resp.SlotToken)
}

func TestAvailable_QuotaExhausted(t *testing.T) {
	h := setupHarness(t)
	// allowance_remaining = 0 means no capacity
	h.seedPartner("tok-full", "partner-C", 0)

	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-full"})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AvailableResponse
	decodeJSON(t, rec, &resp)
	require.False(t, resp.Available)
	require.Empty(t, resp.SlotToken)
}

func TestAvailable_QuotaExhaustedByActiveSectors(t *testing.T) {
	h := setupHarness(t)
	// allowance_remaining = 1, but we'll insert one active pipeline row
	pid := h.seedPartner("tok-active", "partner-D", 1)

	// Insert an active (non-cleaned-up) sector into rseal_provider_pipeline
	_, err := h.db.Exec(context.Background(),
		`INSERT INTO rseal_provider_pipeline (partner_id, sp_id, sector_number, reg_seal_proof) VALUES ($1, 100, 1, 8)`,
		pid)
	require.NoError(t, err)

	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-active"})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp AvailableResponse
	decodeJSON(t, rec, &resp)
	require.False(t, resp.Available)
}

// --- Order + quota decrement tests ---

func TestOrder_AcceptedAndAllowanceDecremented(t *testing.T) {
	h := setupHarness(t)
	pid := h.seedPartner("tok-order", "partner-E", 5)

	// Get a slot token first
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-order"})
	require.Equal(t, http.StatusOK, rec.Code)

	var avail AvailableResponse
	decodeJSON(t, rec, &avail)
	require.True(t, avail.Available)

	// Place the order
	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-order",
		SlotToken:    avail.SlotToken,
		SpID:         200,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.True(t, orderResp.Accepted)

	// Verify allowance went from 5 to 4
	require.Equal(t, int64(4), h.getAllowance(pid))
}

func TestOrder_Idempotent_NoDuplicateDecrement(t *testing.T) {
	h := setupHarness(t)
	pid := h.seedPartner("tok-idem", "partner-F", 5)

	// First order
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-idem"})
	var avail1 AvailableResponse
	decodeJSON(t, rec, &avail1)
	require.True(t, avail1.Available)

	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-idem",
		SlotToken:    avail1.SlotToken,
		SpID:         300,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp1 OrderResponse
	decodeJSON(t, rec, &resp1)
	require.True(t, resp1.Accepted)
	require.Equal(t, int64(4), h.getAllowance(pid))

	// Second order with same (sp_id, sector_number) — should be idempotent.
	// Get a new slot token.
	rec = h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-idem"})
	var avail2 AvailableResponse
	decodeJSON(t, rec, &avail2)
	require.True(t, avail2.Available)

	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-idem",
		SlotToken:    avail2.SlotToken,
		SpID:         300,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp2 OrderResponse
	decodeJSON(t, rec, &resp2)
	require.True(t, resp2.Accepted)

	// Allowance should still be 4, not 3
	require.Equal(t, int64(4), h.getAllowance(pid))
}

func TestOrder_MultipleOrders_AllowanceDecrementsCorrectly(t *testing.T) {
	h := setupHarness(t)
	pid := h.seedPartner("tok-multi", "partner-G", 10)

	for i := int64(1); i <= 3; i++ {
		rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-multi"})
		var avail AvailableResponse
		decodeJSON(t, rec, &avail)
		require.True(t, avail.Available)

		rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
			PartnerToken: "tok-multi",
			SlotToken:    avail.SlotToken,
			SpID:         400,
			SectorNumber: i,
			RegSealProof: 8,
		})
		require.Equal(t, http.StatusOK, rec.Code)

		var orderResp OrderResponse
		decodeJSON(t, rec, &orderResp)
		require.True(t, orderResp.Accepted)
	}

	// 10 - 3 = 7
	require.Equal(t, int64(7), h.getAllowance(pid))
}

// --- Slot token validation tests ---

func TestOrder_ExpiredSlotToken(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-exp", "partner-H", 5)

	// Get a slot token
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-exp"})
	var avail AvailableResponse
	decodeJSON(t, rec, &avail)
	require.True(t, avail.Available)

	// Manually expire the slot by setting its deadline in the past
	h.sm.slotsMu.Lock()
	entry, ok := h.sm.slots[avail.SlotToken]
	require.True(t, ok)
	entry.deadline = time.Now().Add(-1 * time.Second)
	h.sm.slotsMu.Unlock()

	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-exp",
		SlotToken:    avail.SlotToken,
		SpID:         500,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.False(t, orderResp.Accepted)
	require.Contains(t, orderResp.RejectReason, "invalid or expired slot token")
}

func TestOrder_MismatchedPartnerSlotToken(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-mm-A", "partner-mm-A", 5)
	h.seedPartner("tok-mm-B", "partner-mm-B", 5)

	// Get slot token for partner A
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-mm-A"})
	var avail AvailableResponse
	decodeJSON(t, rec, &avail)
	require.True(t, avail.Available)

	// Try to use partner A's slot token with partner B's auth token
	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-mm-B",
		SlotToken:    avail.SlotToken,
		SpID:         600,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.False(t, orderResp.Accepted)
	require.Contains(t, orderResp.RejectReason, "invalid or expired slot token")
}

func TestOrder_ReusedSlotToken(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-reuse", "partner-I", 5)

	// Get a slot token
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-reuse"})
	var avail AvailableResponse
	decodeJSON(t, rec, &avail)
	require.True(t, avail.Available)

	// First order — consumes the slot token
	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-reuse",
		SlotToken:    avail.SlotToken,
		SpID:         700,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp1 OrderResponse
	decodeJSON(t, rec, &resp1)
	require.True(t, resp1.Accepted)

	// Second order with same slot token — should be rejected (single-use)
	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-reuse",
		SlotToken:    avail.SlotToken,
		SpID:         700,
		SectorNumber: 2,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var resp2 OrderResponse
	decodeJSON(t, rec, &resp2)
	require.False(t, resp2.Accepted)
	require.Contains(t, resp2.RejectReason, "invalid or expired slot token")
}

func TestOrder_InvalidSlotToken(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-inv", "partner-J", 5)

	rec := h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-inv",
		SlotToken:    "totally-made-up",
		SpID:         800,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.False(t, orderResp.Accepted)
	require.Contains(t, orderResp.RejectReason, "invalid or expired slot token")
}

// --- Status returns 404 for unknown sector ---

func TestStatus_UnknownSector_404(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-stat", "partner-K", 5)

	rec := h.postJSON(DelegatedSealPath+"status", StatusRequest{
		PartnerToken: "tok-stat",
		SpID:         999,
		SectorNumber: 999,
	})
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Status returns pending for newly ordered sector ---

func TestStatus_PendingSector(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-pend", "partner-L", 5)

	// Get slot + order
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-pend"})
	var avail AvailableResponse
	decodeJSON(t, rec, &avail)
	require.True(t, avail.Available)

	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-pend",
		SlotToken:    avail.SlotToken,
		SpID:         900,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.True(t, orderResp.Accepted)

	// Poll status
	rec = h.postJSON(DelegatedSealPath+"status", StatusRequest{
		PartnerToken: "tok-pend",
		SpID:         900,
		SectorNumber: 1,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var status StatusResponse
	decodeJSON(t, rec, &status)
	require.Equal(t, "pending", status.State)
}

// --- Capabilities (no auth required) ---

func TestCapabilities(t *testing.T) {
	h := setupHarness(t)

	req := httptest.NewRequest(http.MethodGet, DelegatedSealPath+"capabilities", nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp CapabilitiesResponse
	decodeJSON(t, rec, &resp)
	require.True(t, len(resp.SupportedProofs) > 0)
	require.True(t, resp.SupportsRangeRequests)
}

// --- Cross-partner isolation: partner A cannot see partner B's sectors ---

func TestStatus_CrossPartnerIsolation(t *testing.T) {
	h := setupHarness(t)
	h.seedPartner("tok-iso-a", "partner-iso-A", 5)
	h.seedPartner("tok-iso-b", "partner-iso-B", 5)

	// Partner A orders a sector
	rec := h.postJSON(DelegatedSealPath+"available", AuthorizeRequest{PartnerToken: "tok-iso-a"})
	var avail AvailableResponse
	decodeJSON(t, rec, &avail)

	rec = h.postJSON(DelegatedSealPath+"order", OrderRequest{
		PartnerToken: "tok-iso-a",
		SlotToken:    avail.SlotToken,
		SpID:         1000,
		SectorNumber: 1,
		RegSealProof: 8,
	})
	var orderResp OrderResponse
	decodeJSON(t, rec, &orderResp)
	require.True(t, orderResp.Accepted)

	// Partner B tries to check status of partner A's sector — should get 404
	rec = h.postJSON(DelegatedSealPath+"status", StatusRequest{
		PartnerToken: "tok-iso-b",
		SpID:         1000,
		SectorNumber: 1,
	})
	require.Equal(t, http.StatusNotFound, rec.Code)
}
