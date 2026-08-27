package ipni_provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
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

func newTestProvider(t *testing.T, serviceURLs ...string) *Provider {
	t.Helper()
	urls := make([]*url.URL, len(serviceURLs))
	for i, s := range serviceURLs {
		u, err := url.Parse(s)
		require.NoError(t, err)
		urls[i] = u
	}
	return &Provider{
		serviceURLs: urls,
		syncClient:  &http.Client{Timeout: 5 * time.Second},
	}
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
