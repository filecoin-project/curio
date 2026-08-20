package ipni_provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

// testAdCid is a syntactically valid CIDv1; its content is irrelevant to these tests,
// only its string form (used as a URL path segment) matters.
const testAdCid = "baguqeeraopdunfoiljzoxrn2ozzmi2ndzq3npr5rmpfjxxvfvenwscsxsyva"

// secondTestAdCid is a second syntactically valid CIDv1, distinct from testAdCid.
const secondTestAdCid = "bafkreiezij5trhw6lwpydyui3nkzyihoaaawij5shmnk3azee7pisdetbu"

func mustParseTestCid(t *testing.T) cid.Cid {
	t.Helper()
	c, err := cid.Parse(testAdCid)
	require.NoError(t, err)
	return c
}

func mustParseSecondTestCid(t *testing.T) cid.Cid {
	t.Helper()
	c, err := cid.Parse(secondTestAdCid)
	require.NoError(t, err)
	return c
}

func adSyncStatusHandler(t *testing.T, indexed bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: indexed})
	}
}

func newTestProvider(t *testing.T, serviceURLs []string, providerInfos map[string]*peerInfo, latest map[string]cid.Cid) *Provider {
	t.Helper()
	urls := make([]*url.URL, len(serviceURLs))
	for i, s := range serviceURLs {
		u, err := url.Parse(s)
		require.NoError(t, err)
		urls[i] = u
	}
	return &Provider{
		providerInfos: providerInfos,
		latest:        latest,
		serviceURLs:   urls,
		httpClient:    &http.Client{Timeout: 5 * time.Second},
	}
}

func TestCheckSyncStatus_ConfirmsAndAdvancesWatermark(t *testing.T) {
	srv := httptest.NewServer(adSyncStatusHandler(t, true))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now().Add(-time.Minute)
	peer := "peer1"
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(5), on)
	require.NotNil(t, at)
	require.True(t, at.After(announcedAt))
}

func TestCheckSyncStatus_SeededWatermarkWithoutAnnouncedAtStillAdvances(t *testing.T) {
	srv := httptest.NewServer(adSyncStatusHandler(t, true))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	peer := "peer1"
	// Simulates the post-restart state: announcedOrderNumber was seeded from the
	// DB, but announcedAt is nil because the true announce time is unknown.
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: nil},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(5), on, "sync watermark must still advance even without a known announce time")
	require.NotNil(t, at)
}

func TestCheckSyncStatus_NotYetIndexedDoesNotAdvance(t *testing.T) {
	srv := httptest.NewServer(adSyncStatusHandler(t, false))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now()
	peer := "peer1"
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(0), on)
	require.Nil(t, at)
}

func TestCheckSyncStatus_SkipsServicesThatDontSupportEndpoint(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()
	ok := httptest.NewServer(adSyncStatusHandler(t, true))
	defer ok.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now()
	peer := "peer1"
	// notFound listed first: the 404 must be skipped, not treated as "not indexed".
	p := newTestProvider(t, []string{notFound.URL, ok.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 3, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(3), on)
	require.NotNil(t, at)
}

func TestCheckSyncStatus_NoOpWhenAlreadySynced(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: true})
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now()
	syncedAt := time.Now()
	peer := "peer1"
	// announcedOrderNumber == syncedOrderNumber: nothing new to confirm.
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: &announcedAt, syncedOrderNumber: 5, syncedAt: &syncedAt},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	require.False(t, called, "should not query indexer service when already synced up to the announced head")
}

func TestCheckSyncStatus_SkipsWhenAlreadyRunning(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: true})
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now()
	peer := "peer1"
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: adCid})

	// Simulate a run already in flight, same as if a previous tick's
	// checkSyncStatus hadn't returned yet.
	p.checkingSyncStatus.Store(true)

	p.checkSyncStatus(context.Background())

	require.False(t, called, "should not query indexer service when a check is already in flight")
	on, _ := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(0), on)
}

func TestCheckSyncStatus_PinsTargetUntilConfirmed(t *testing.T) {
	oldCid := mustParseTestCid(t)
	newCid := mustParseSecondTestCid(t)

	indexed := false // whether oldCid has been confirmed yet
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedCid := path.Base(r.URL.Path)
		requested = append(requested, requestedCid)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: requestedCid, Indexed: indexed && requestedCid == oldCid.String()})
	}))
	defer srv.Close()

	announcedAt := time.Now().Add(-time.Minute)
	peer := "peer1"
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 10, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: oldCid})

	// Pins the checkpoint at order 10 / oldCid; not indexed yet.
	p.checkSyncStatus(context.Background())
	require.Equal(t, []string{oldCid.String()}, requested)

	// A newer head lands before the pinned target confirms.
	newAnnouncedAt := time.Now()
	p.mu.Lock()
	p.providerInfos[peer].announcedOrderNumber = 12
	p.providerInfos[peer].announcedAt = &newAnnouncedAt
	p.latest[peer] = newCid
	p.mu.Unlock()

	// Must keep checking the pinned target, not redirect to the new head.
	p.checkSyncStatus(context.Background())
	require.Equal(t, []string{oldCid.String(), oldCid.String()}, requested)
	on, _ := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(0), on)

	// The indexer catches up on the pinned target.
	indexed = true
	p.checkSyncStatus(context.Background())
	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(10), on)
	require.NotNil(t, at)

	// The next checkpoint jumps straight to the current head (order 12), not
	// order 11.
	p.checkSyncStatus(context.Background())
	require.Equal(t, []string{oldCid.String(), oldCid.String(), oldCid.String(), newCid.String()}, requested)
}

func TestCheckSyncStatus_SkippedAdAdvancesWatermark(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: false, State: "skipped", SkipReason: "malformed"})
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	announcedAt := time.Now()
	peer := "peer1"
	p := newTestProvider(t, []string{srv.URL}, map[string]*peerInfo{
		peer: {announcedOrderNumber: 5, announcedAt: &announcedAt},
	}, map[string]cid.Cid{peer: adCid})

	p.checkSyncStatus(context.Background())

	on, at := p.SyncedOrderNumber(peer)
	require.Equal(t, int64(5), on, "watermark must advance past a permanently skipped ad, not wait forever")
	require.NotNil(t, at)
}
