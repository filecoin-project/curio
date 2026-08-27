package ipni_provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"
)

// testAdCid is a syntactically valid CIDv1; its content is irrelevant to these tests,
// only its string form (used as a URL path segment) matters.
const testAdCid = "baguqeeraopdunfoiljzoxrn2ozzmi2ndzq3npr5rmpfjxxvfvenwscsxsyva"

func mustParseTestCid(t *testing.T) cid.Cid {
	t.Helper()
	c, err := cid.Parse(testAdCid)
	require.NoError(t, err)
	return c
}

// makeTestCid builds a distinct valid CID from seed.
func makeTestCid(t *testing.T, seed byte) cid.Cid {
	t.Helper()
	sum, err := mh.Sum([]byte{seed}, mh.SHA2_256, -1)
	require.NoError(t, err)
	return cid.NewCidV1(cid.Raw, sum)
}

func newTestProvider(t *testing.T, serviceURLs ...string) *Provider {
	t.Helper()
	urls := make([]*url.URL, len(serviceURLs))
	for i, s := range serviceURLs {
		u, err := url.Parse(s)
		require.NoError(t, err)
		urls[i] = u
	}
	return &Provider{
		serviceURLs:  urls,
		syncClient:   &http.Client{Timeout: 5 * time.Second},
		syncCache:    must.One(lru.New[cid.Cid, syncResult](syncCacheSize)),
		syncCheckSem: make(chan struct{}, syncCheckConcurrency),
	}
}

// waitForInFlightCheckToFinish polls until adCid is no longer marked in-flight.
func waitForInFlightCheckToFinish(t *testing.T, p *Provider, adCid cid.Cid) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, inFlight := p.syncChecksInFlight.Load(adCid); !inFlight {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for async sync check to finish")
}

func TestQueryAdSyncStatus_Indexed(t *testing.T) {
	indexedTime := time.Date(2026, 8, 26, 11, 49, 52, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: true, IndexedTime: &indexedTime})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	service, status, ok := p.queryAdSyncStatus(context.Background(), mustParseTestCid(t))
	require.True(t, ok)
	require.Equal(t, srv.URL, service)
	require.True(t, status.Indexed)
	require.False(t, status.Skipped)
	require.True(t, status.IndexedTime.Equal(indexedTime))
}

func TestQueryAdSyncStatus_Skipped(t *testing.T) {
	skippedTime := time.Date(2026, 8, 26, 11, 49, 52, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Skipped: true, SkippedTime: &skippedTime})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, status, ok := p.queryAdSyncStatus(context.Background(), mustParseTestCid(t))
	require.True(t, ok)
	require.True(t, status.Skipped)
	require.False(t, status.Indexed)
}

func TestQueryAdSyncStatus_StillPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, ok := p.queryAdSyncStatus(context.Background(), mustParseTestCid(t))
	require.False(t, ok)
}

func TestQueryAdSyncStatus_404FallsThroughToNextService(t *testing.T) {
	notSupported := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notSupported.Close()

	supported := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Indexed: true})
	}))
	defer supported.Close()

	p := newTestProvider(t, notSupported.URL, supported.URL)
	service, status, ok := p.queryAdSyncStatus(context.Background(), mustParseTestCid(t))
	require.True(t, ok)
	require.True(t, status.Indexed)
	require.Equal(t, supported.URL, service)
}

func TestQueryAdSyncStatus_NoServiceConfirms(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	service, _, ok := p.queryAdSyncStatus(context.Background(), mustParseTestCid(t))
	require.False(t, ok)
	require.Empty(t, service)
}

func TestSyncedAt_CacheHitReturnsImmediatelyWithoutQuerying(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		t.Fatal("cache hit should not query the indexer")
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	indexedAt := time.Date(2026, 8, 26, 11, 49, 52, 0, time.UTC)
	p := newTestProvider(t, srv.URL)
	p.syncCache.Add(adCid, syncResult{IndexedAt: &indexedAt})

	got := p.SyncedAt(adCid, "provider")
	require.NotNil(t, got)
	require.True(t, got.Equal(indexedAt))
	require.Zero(t, atomic.LoadInt32(&requests))
}

func TestSyncedAt_CacheMissReturnsNilAndPopulatesCacheInBackground(t *testing.T) {
	skippedTime := time.Date(2026, 8, 26, 11, 49, 52, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Skipped: true, SkippedTime: &skippedTime})
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	p := newTestProvider(t, srv.URL)

	got := p.SyncedAt(adCid, "provider")
	require.Nil(t, got, "a cache miss must return nil immediately rather than block on the check")

	waitForInFlightCheckToFinish(t, p, adCid)

	sr, ok := p.syncCache.Get(adCid)
	require.True(t, ok, "the background check should have populated the cache")
	require.Nil(t, sr.IndexedAt, "a skipped ad has no indexed time")

	// still nil: skipped ads have no indexed time
	require.Nil(t, p.SyncedAt(adCid, "provider"))
}

func TestSyncedAt_DedupesConcurrentChecksForSameAdCid(t *testing.T) {
	var requests int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Ad: testAdCid, Skipped: true})
	}))
	defer srv.Close()

	adCid := mustParseTestCid(t)
	p := newTestProvider(t, srv.URL)

	// concurrent lookups for the same ad_cid; only one should reach the indexer
	for i := 0; i < 5; i++ {
		require.Nil(t, p.SyncedAt(adCid, "provider"))
	}
	close(release)
	waitForInFlightCheckToFinish(t, p, adCid)

	require.EqualValues(t, 1, atomic.LoadInt32(&requests))
}

func TestCheckAdSyncStatusAsync_SkipsRatherThanQueuesWhenAtCapacity(t *testing.T) {
	const limit = 2
	const numAds = 5

	var requests int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adSyncStatus{Skipped: true})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	p.syncCheckSem = make(chan struct{}, limit)

	adCids := make([]cid.Cid, numAds)
	for i := range adCids {
		adCids[i] = makeTestCid(t, byte(i))
		p.checkAdSyncStatusAsync(adCids[i], "provider")
	}

	// the first `limit` calls hold the semaphore; the rest were skipped outright
	inFlight := 0
	for _, adCid := range adCids {
		if _, ok := p.syncChecksInFlight.Load(adCid); ok {
			inFlight++
		}
	}
	require.Equal(t, limit, inFlight, "only the first `limit` checks should still be in flight, the rest were skipped, not queued")

	close(release)
	for i := 0; i < limit; i++ {
		waitForInFlightCheckToFinish(t, p, adCids[i])
	}
	require.EqualValues(t, limit, atomic.LoadInt32(&requests), "skipped checks must never reach the server")
}
