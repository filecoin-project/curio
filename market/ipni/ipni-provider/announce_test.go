package ipni_provider

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

// newAnnounceTestProvider builds a Provider with one PDP provider (SPID 0, so no announce
// URL is filtered out) announcing to the given URLs.
func newAnnounceTestProvider(t *testing.T, announceURLs ...string) (*Provider, string) {
	t.Helper()

	priv, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)
	addr, err := multiaddr.NewMultiaddr("/dns/example.com/tcp/443/https")
	require.NoError(t, err)

	urls := make([]*url.URL, len(announceURLs))
	for i, s := range announceURLs {
		u, err := url.Parse(s)
		require.NoError(t, err)
		urls[i] = u
	}

	p := &Provider{
		announceURLs: urls,
		providerInfos: map[string]*peerInfo{
			id.String(): {
				ID:                  id,
				Key:                 priv,
				SPID:                0, // PDP-only: announces to every configured URL
				httpServerAddresses: addr,
				providerType:        PDPv1ProviderType,
			},
		},
		latest: make(map[string]cid.Cid),
	}
	return p, id.String()
}

// countingIndexer is an httptest server standing in for an indexer's /announce endpoint.
func countingIndexer(t *testing.T, status int) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// An unreachable indexer must not stop a reachable one from receiving the announce,
// whichever status it rejects with. Anything that is not 200/204 is one code path.
func TestPublishHTTP_DeadIndexerDoesNotBlockHealthyIndexer(t *testing.T) {
	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusGone,
		http.StatusNotFound,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			dead, deadHits := countingIndexer(t, status)
			healthy, healthyHits := countingIndexer(t, http.StatusOK)

			p, provider := newAnnounceTestProvider(t, dead.URL, healthy.URL)

			delivered, err := p.publishhttp(context.Background(), makeTestCid(t, 1), provider)
			require.Error(t, err, "the dead indexer's failure should still be reported")
			require.Equal(t, 1, delivered, "exactly one indexer accepted the announce")
			require.EqualValues(t, 1, atomic.LoadInt32(deadHits))
			require.EqualValues(t, 1, atomic.LoadInt32(healthyHits), "the healthy indexer must still receive the announce")
		})
	}
}

// One unreachable indexer must not defeat the duplicate-announce guard: an unchanged head
// is announced to the reachable indexers once, not once per publish tick.
func TestAnnounceProviderHead_DeadIndexerDoesNotCauseRepublishToHealthyIndexer(t *testing.T) {
	dead, _ := countingIndexer(t, http.StatusInternalServerError)
	healthy, healthyHits := countingIndexer(t, http.StatusOK)

	p, provider := newAnnounceTestProvider(t, dead.URL, healthy.URL)
	head := makeTestCid(t, 1)

	// three publish ticks with an unchanged head
	for i := 0; i < 3; i++ {
		p.announceProviderHead(context.Background(), provider, head)
	}

	require.EqualValues(t, 1, atomic.LoadInt32(healthyHits),
		"an unchanged head must be announced to the healthy indexer once, not once per tick")
	require.NotNil(t, p.LastPublishTime(provider),
		"a head accepted by at least one indexer counts as published")
}

// A new head is announced even while one indexer is unreachable.
func TestAnnounceProviderHead_NewHeadIsStillAnnouncedWhileOneIndexerIsDown(t *testing.T) {
	dead, _ := countingIndexer(t, http.StatusInternalServerError)
	healthy, healthyHits := countingIndexer(t, http.StatusOK)

	p, provider := newAnnounceTestProvider(t, dead.URL, healthy.URL)

	p.announceProviderHead(context.Background(), provider, makeTestCid(t, 1))
	p.announceProviderHead(context.Background(), provider, makeTestCid(t, 2))

	require.EqualValues(t, 2, atomic.LoadInt32(healthyHits))
}

// With no indexer accepting the announce there is nothing to dedupe against, so every
// tick retries.
func TestAnnounceProviderHead_RetriesWhenNoIndexerAccepted(t *testing.T) {
	deadA, hitsA := countingIndexer(t, http.StatusInternalServerError)
	deadB, hitsB := countingIndexer(t, http.StatusBadGateway)

	p, provider := newAnnounceTestProvider(t, deadA.URL, deadB.URL)
	head := makeTestCid(t, 1)

	for i := 0; i < 3; i++ {
		p.announceProviderHead(context.Background(), provider, head)
	}

	require.EqualValues(t, 3, atomic.LoadInt32(hitsA), "a totally failed announce must be retried")
	require.EqualValues(t, 3, atomic.LoadInt32(hitsB))
	require.Nil(t, p.LastPublishTime(provider), "nothing was published")
}

// With every indexer reachable, an unchanged head is announced once.
func TestAnnounceProviderHead_DedupesWhenAllIndexersHealthy(t *testing.T) {
	healthy, hits := countingIndexer(t, http.StatusOK)

	p, provider := newAnnounceTestProvider(t, healthy.URL)
	head := makeTestCid(t, 1)

	for i := 0; i < 3; i++ {
		p.announceProviderHead(context.Background(), provider, head)
	}

	require.EqualValues(t, 1, atomic.LoadInt32(hits))
}

// An indexer that accepts the connection and then never answers must not hold up delivery
// to the others. The announce context bounds the wait, well inside publishhttp's client
// timeout.
func TestPublishHTTP_HangingIndexerDoesNotBlockReachableIndexer(t *testing.T) {
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(hanging.Close)
	t.Cleanup(func() { close(release) })

	reachable, reachableHits := countingIndexer(t, http.StatusOK)
	p, provider := newAnnounceTestProvider(t, hanging.URL, reachable.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	delivered, err := p.publishhttp(ctx, makeTestCid(t, 1), provider)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Equal(t, 1, delivered, "the reachable indexer accepted the announce")
	require.EqualValues(t, 1, atomic.LoadInt32(reachableHits))
	require.Less(t, elapsed, 5*time.Second, "the announce must not wait out the client timeout")
}
