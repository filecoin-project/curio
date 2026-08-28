package itests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

const (
	ipniSummaryPeerID = "12D3KooWPRuubsz8p49yAFBZGwsKVEF9MqxN1CQ1QRMeaDE4VCZu"
	ipniSummaryAdCid  = "baguqeeraopdunfoiljzoxrn2ozzmi2ndzq3npr5rmpfjxxvfvenwscsxsyva"
)

// seedIPNIProvider inserts one IPNI provider with an advertisement chain head.
func seedIPNIProvider(ctx context.Context, t *testing.T, db *harmonydb.DB) {
	t.Helper()

	_, err := db.Exec(ctx, `INSERT INTO ipni (ad_cid, context_id, is_rm, provider, addresses, signature, entries, piece_cid, piece_size)
		VALUES ($1, '\x00', false, $2, '', '\x00', $1, 'x', 1)`, ipniSummaryAdCid, ipniSummaryPeerID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `INSERT INTO ipni_head (provider, head) VALUES ($1, $2)`, ipniSummaryPeerID, ipniSummaryAdCid)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `INSERT INTO ipni_peerid (priv_key, peer_id, sp_id) VALUES ('\x01', $1, 0)`, ipniSummaryPeerID)
	require.NoError(t, err)
}

// setIPNIServiceURLs points the IPNI summary at the given indexer services.
func setIPNIServiceURLs(ctx context.Context, t *testing.T, db *harmonydb.DB, urls ...string) {
	t.Helper()

	quoted, err := json.Marshal(urls)
	require.NoError(t, err)

	layer := fmt.Sprintf(`
[Market]
  [Market.StorageMarketConfig]
    [Market.StorageMarketConfig.IPNI]
      ServiceURL = %s
`, string(quoted))

	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ('itest-ipni-summary', $1)`, layer)
	require.NoError(t, err)
}

// reachableIndexer answers /providers/{peerID} the way an indexer does.
func reachableIndexer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(webrpc.ParsedResponse{
			AddrInfo:          webrpc.AddrInfo{ID: ipniSummaryPeerID, Addrs: []string{"/dns/example.com/tcp/443/https"}},
			LastAdvertisement: webrpc.Advertisement{Slash: ipniSummaryAdCid},
			Publisher:         webrpc.AddrInfo{ID: ipniSummaryPeerID},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An indexer service that cannot be reached must not take down the whole IPNI summary:
// the failure is reported against that service and the reachable services still report.
func TestIPNISummaryUnreachableIndexerService(t *testing.T) {
	ctx := context.Background()

	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	seedIPNIProvider(ctx, t, db)

	reachable := reachableIndexer(t)

	// a server that is closed immediately refuses connections, the way a decommissioned
	// indexer hostname does
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	downURL := down.URL
	down.Close()

	setIPNIServiceURLs(ctx, t, db, downURL, reachable.URL)

	a := &webrpc.WebRPC{Deps: &deps.Deps{DB: db}}

	summary, err := a.IPNISummary(ctx)
	require.NoError(t, err, "an unreachable indexer service must not fail the whole summary")
	require.Len(t, summary, 1)

	byService := map[string]webrpc.IpniSyncStatus{}
	for _, s := range summary[0].SyncStatus {
		byService[s.Service] = s
	}

	require.Contains(t, byService, reachable.URL, "the reachable service must still be reported")
	require.Equal(t, ipniSummaryAdCid, byService[reachable.URL].RemoteAd)

	require.Contains(t, byService, downURL, "the unreachable service must be reported as failing")
	require.NotEmpty(t, byService[downURL].Error)
}
