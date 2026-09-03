package ipni_provider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce"
	"github.com/ipni/go-libipni/announce/httpsender"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/urlhelper"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

// IPNIRoutePath is a constant representing the route path for IPNI provider.
const IPNIRoutePath = "/ipni-provider/"

// IPNIPath is a constant that represents the path for IPNI API requests.
const IPNIPath = "/ipni/v1/ad/"

const PublishInterval = 1 * time.Second

// interProviderAnnounceDelay spaces out announces when a node runs multiple providers.
const interProviderAnnounceDelay = 200 * time.Millisecond

const (
	PDPv0ProviderType = "PDP_v0"
	PDPv1ProviderType = "PDP_v1"
	PoRepProviderType = "PoRep"
)

// syncCacheSize caps the in-memory ad-sync-status cache.
const syncCacheSize = 10000

// syncCheckConcurrency caps concurrent ad-sync-status checks against the indexer.
const syncCheckConcurrency = 20

var (
	log = logging.Logger("ipni-provider")
)

// peerInfo represents information about a peer, including its ID and private key.
type peerInfo struct {
	ID    peer.ID
	Key   crypto.PrivKey
	SPID  abi.ActorID
	Miner address.Address
	// httpServerAddresses has a list of all the addresses where IPNI can reach to sync with
	// the provider. This is created by converting announceURLs into a multiaddr and adding the following
	// Curio HTTP URL(in multiaddr)+IPNIRoutePath(/ipni-provider/)+peerID
	httpServerAddresses multiaddr.Multiaddr // map[peerID String]Multiaddr
	providerType        string
	lastPublishTime     *time.Time
}

// Provider represents a provider for IPNI.
type Provider struct {
	full          api.Chain
	db            *harmonydb.DB
	pieceProvider *pieceprovider.SectorReader
	indexStore    *indexstore.IndexStore
	sc            *chunker.ServeChunker
	mu            sync.RWMutex
	providerInfos map[string]*peerInfo // map[peerID String]peerInfo
	latest        map[string]cid.Cid   // map[peerID String]last published head, used to avoid duplicate announce
	// announceURLs enables sending direct announcements via HTTP. This is
	// the list of indexer URLs to send direct HTTP announce messages to.
	announceURLs   []*url.URL
	httpServerBase *url.URL
	startLock      sync.Once

	// serviceURLs are indexer services checked for ad sync status, e.g.
	// https://cid.contact. Not all of them support that endpoint.
	serviceURLs []*url.URL
	syncClient  *http.Client
	// syncCache holds confirmed ad indexed times, populated lazily on request; nil for a skipped ad.
	syncCache *lru.Cache[cid.Cid, *time.Time]
	// syncChecksInFlight dedups concurrent checks for the same ad_cid.
	syncChecksInFlight sync.Map
	// syncCheckSem bounds how many ad-sync-status checks run at once, across all ad_cids.
	syncCheckSem chan struct{}
}

// NewProvider initializes a new Provider using the provided dependencies.
// It retrieves private libp2p keys from the database, unmarshals them, generates peer IDs,
// and populates a keyMap with the corresponding peer information.
// It also sets up the announce URLs and HTTP server addresses based on the configuration.
// The Provider struct is then created with the populated fields and returned along with nil error.
// If any error occurs during the process, it is returned along with a non-nil Provider.
func NewProvider(d *deps.Deps) (*Provider, error) {
	ctx := context.Background()

	announceURLs, err := parseURLs(d.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs)
	if err != nil {
		return nil, err
	}

	baseURL, err := urlhelper.GetExternalURL(&d.Cfg.HTTP)
	if err != nil {
		return nil, xerrors.Errorf("getting external URL for IPNI: %w", err)
	}
	baseURL.Path = path.Join(baseURL.Path, IPNIRoutePath)

	serviceURLs, err := parseURLs(d.Cfg.Market.StorageMarketConfig.IPNI.ServiceURL)
	if err != nil {
		return nil, err
	}

	p := &Provider{
		full:           d.Chain,
		db:             d.DB,
		pieceProvider:  d.SectorReader,
		indexStore:     d.IndexStore,
		sc:             d.ServeChunker,
		providerInfos:  make(map[string]*peerInfo),
		announceURLs:   announceURLs,
		httpServerBase: baseURL,
		latest:         make(map[string]cid.Cid),
		serviceURLs:    serviceURLs,
		syncClient:     &http.Client{Timeout: 5 * time.Second},
		syncCache:      must.One(lru.New[cid.Cid, *time.Time](syncCacheSize)),
		syncCheckSem:   make(chan struct{}, syncCheckConcurrency),
	}

	if err := p.refreshProviders(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

func parseURLs(strs []string) ([]*url.URL, error) {
	urls := make([]*url.URL, len(strs))
	for i, s := range strs {
		u, err := url.Parse(s)
		if err != nil {
			return nil, xerrors.Errorf("parsing URL %q: %w", s, err)
		}
		urls[i] = u
	}
	return urls, nil
}

// insertProvider inserts new providers and updates the providerInfos map
// It does not change the details of the existing provider as all detailed are tied to a private key.
// To remove an existing provider, a restart of all market nodes is required.
func (p *Provider) insertProvider(priv []byte, peerID string, sp int64) error {
	pkey, err := crypto.UnmarshalPrivateKey(priv)
	if err != nil {
		return xerrors.Errorf("unmarshaling private key: %w", err)
	}

	id, err := peer.IDFromPublicKey(pkey.GetPublic())
	if err != nil {
		return xerrors.Errorf("generating peer ID from private key: %w", err)
	}
	if id.String() != peerID {
		return xerrors.Errorf("peer ID mismatch: got %s (calculated), expected %s (DB)", id.String(), peerID)
	}

	var spID abi.ActorID
	if sp >= 0 {
		spID = abi.ActorID(sp)
	}

	maddr, err := address.NewIDAddress(uint64(spID))
	if err != nil {
		return xerrors.Errorf("parsing miner ID: %w", err)
	}

	u := *p.httpServerBase
	u.Path = path.Join(u.Path, peerID)
	addr, err := urlhelper.FromURLWithPort(&u)
	if err != nil {
		return xerrors.Errorf("converting URL to multiaddr: %w", err)
	}

	info := &peerInfo{
		Key:                 pkey,
		ID:                  id,
		SPID:                spID,
		Miner:               maddr,
		httpServerAddresses: addr,
	}

	switch sp {
	case -1:
		info.providerType = PDPv1ProviderType
	case -2:
		info.providerType = PDPv0ProviderType
	default:
		info.providerType = PoRepProviderType
	}

	// Let's double-check before we update
	p.mu.Lock()
	_, existed := p.providerInfos[peerID]
	if !existed {
		log.Infow("New IPNI provider discovered", "peerID", id.String(), "spID", spID.String(), "miner", maddr.String(), "url", u.String())
		p.providerInfos[peerID] = info
	}
	p.mu.Unlock()

	return nil
}

func (p *Provider) refreshProviders(ctx context.Context) error {
	rows, err := p.db.Query(ctx, `SELECT priv_key, peer_id, sp_id FROM ipni_peerid`)
	if err != nil {
		return xerrors.Errorf("failed to refresh ipni peers from DB: %w", err)
	}
	defer rows.Close()

	p.mu.RLock()
	peers := slices.Sorted(maps.Keys(p.providerInfos))
	p.mu.RUnlock()

	for rows.Next() && rows.Err() == nil {
		var priv []byte
		var peerID string
		var sp int64
		if err := rows.Scan(&priv, &peerID, &sp); err != nil {
			return xerrors.Errorf("failed to scan refreshed ipni peer row: %w", err)
		}

		if lo.Contains(peers, strings.TrimSpace(peerID)) {
			continue
		}
		if err := p.insertProvider(priv, peerID, sp); err != nil {
			return err
		}
	}

	if rows.Err() != nil {
		return rows.Err()
	}

	return nil
}

// getAd retrieves an advertisement from the database based on the given CID and provider.
// It returns the advertisement and an error, if any.
func (p *Provider) getAd(ctx context.Context, ad cid.Cid, provider string) (schema.Advertisement, error) {
	var ads []struct {
		ContextID []byte
		IsRm      bool
		Previous  *string
		Provider  string
		Addresses string
		Signature []byte
		Entries   string
		Metadata  []byte
	}

	err := p.db.Select(ctx, &ads, `SELECT 
										context_id,
										is_rm, 
										previous, 
										provider, 
										addresses, 
										signature, 
										entries,
										metadata
										FROM ipni 
										WHERE ad_cid = $1 
										  AND provider = $2`, ad.String(), provider)

	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("getting ad from DB: %w", err)
	}

	if len(ads) == 0 {
		return schema.Advertisement{}, chunker.ErrNotFound
	}

	if len(ads) > 1 {
		return schema.Advertisement{}, xerrors.Errorf("expected 1 ad but got %d", len(ads))
	}

	a := ads[0]

	e, err := cid.Parse(a.Entries)
	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("parsing entry CID: %w", err)
	}

	adv := schema.Advertisement{
		Provider:  a.Provider,
		Signature: a.Signature,
		Entries:   cidlink.Link{Cid: e},
		ContextID: a.ContextID,
		IsRm:      a.IsRm,
		Metadata:  a.Metadata,
	}

	if a.Addresses != "" {
		adv.Addresses = strings.Split(a.Addresses, "|")
	}

	if a.Previous != nil {
		prev, err := cid.Parse(*a.Previous)
		if err != nil {
			return schema.Advertisement{}, xerrors.Errorf("parsing previous CID: %w", err)
		}

		adv.PreviousID = cidlink.Link{Cid: prev}
	}

	{
		nd, err := adv.ToNode()
		if err != nil {
			return schema.Advertisement{}, xerrors.Errorf("converting advertisement to node: %w", err)
		}

		al, err := ipniculib.NodeToLink(nd, schema.Linkproto)
		if err != nil {
			return schema.Advertisement{}, xerrors.Errorf("converting node to link: %w", err)
		}

		if al.String() != ad.String() {
			log.Errorw("advertisement node does not match the expected advertisement CID", "got", al.String(), "expected", ad.String(), "adv", adv)
			return schema.Advertisement{}, xerrors.Errorf("advertisement node does not match the expected advertisement CID, got %s, expected %s", al.String(), ad.String())
		}
	}

	return adv, nil
}

func (p *Provider) getAdBytes(ctx context.Context, ad cid.Cid, provider string) ([]byte, error) {
	a, err := p.getAd(ctx, ad, provider)
	if err != nil {
		return nil, err
	}

	adn, err := a.ToNode()
	if err != nil {
		return nil, err
	}

	// Use local buffer for better error handing
	resp := new(bytes.Buffer)
	err = dagjson.Encode(adn, resp)
	if err != nil {
		return nil, xerrors.Errorf("failed to encode ad %s for peer %s: %w", ad.String(), provider, err)
	}

	return resp.Bytes(), nil
}

// getHead retrieves the head of a provider from the database, generates a signed head, and returns it as encoded bytes.
// If the head is not found or if there is an error, it returns an appropriate error.
func (p *Provider) getHead(ctx context.Context, provider string) ([]byte, error) {
	var headStr string
	err := p.db.QueryRow(ctx, `SELECT head FROM ipni_head WHERE provider = $1`, provider).Scan(&headStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, chunker.ErrNotFound
		}
		return nil, xerrors.Errorf("querying previous head: %w", err)
	}

	if headStr == "" {
		return nil, chunker.ErrNotFound
	}

	ad, err := cid.Parse(headStr)
	if err != nil {
		return nil, xerrors.Errorf("parsing head CID: %w", err)
	}

	p.mu.RLock()
	info, ok := p.providerInfos[provider]
	p.mu.RUnlock()
	if !ok {
		if err := p.refreshProviders(ctx); err != nil {
			return nil, err
		}
		p.mu.RLock()
		info, ok = p.providerInfos[provider]
		p.mu.RUnlock()
		if !ok {
			return nil, xerrors.Errorf("no info found for provider %s", provider)
		}
	}

	signedHead, err := head.NewSignedHead(ad, "", info.Key)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate signed head for peer %s: %w", provider, err)
	}

	return signedHead.Encode()
}

func (p *Provider) handleGetHead(w http.ResponseWriter, r *http.Request) {
	log.Infow("Received IPNI request", "path", r.URL.Path)

	start := time.Now()
	providerID := chi.URLParam(r, "providerId")
	mw := &metricResponseWriter{ResponseWriter: w}
	w = mw
	defer func() {
		recordProviderHTTPRequest(providerID, "head", mw.Status(), time.Since(start))
	}()

	sh, err := p.getHead(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, chunker.ErrNotFound) {
			log.Warnw("No Content Found", "providerId", providerID)
			http.Error(w, "", http.StatusNoContent)
			return
		}
		log.Errorf("failed to get signed head for peer %s: %v", providerID, err)
		http.Error(w, "", http.StatusInternalServerError)
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(sh)
	if err != nil {
		log.Errorw("failed to write HTTP response", "err", err)
	}
}

// handleGet handles GET requests.
func (p *Provider) handleGet(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	reqCid := chi.URLParam(r, "cid")

	start := time.Now()
	content := "unknown"
	mw := &metricResponseWriter{ResponseWriter: w}
	w = mw

	defer func() {
		log.Infow("Served IPNI request", "path", r.URL.Path, "cid", reqCid, "providerId", providerID, "took", time.Since(start), "remote_addr", r.RemoteAddr)
		recordProviderHTTPRequest(providerID, content, mw.Status(), time.Since(start))
	}()

	b, err := cid.Parse(reqCid)
	if err != nil {
		log.Warnw("invalid CID as path parameter while getting content", "request", reqCid, "err", err)
		http.Error(w, "invalid CID: "+reqCid, http.StatusBadRequest)
		return
	}

	h := r.Header.Get(ipnisync.CidSchemaHeader)

	switch h {
	case ipnisync.CidSchemaAdvertisement:
		content = "advertisement"
		ad, err := p.getAdBytes(r.Context(), b, providerID)
		if err != nil {
			if errors.Is(err, chunker.ErrNotFound) {
				log.Warnw("No Content Found", "CID", b.String())
				http.Error(w, "", http.StatusNoContent)
				return
			}
			log.Errorf("failed to get advertisement %s for peer %s: %v", b.String(), providerID, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(ad)
		if err != nil {
			log.Errorw("failed to write HTTP response", "err", err)
		}
		return
	case ipnisync.CidSchemaEntryChunk:
		content = "entry"
		entry, err := p.sc.GetEntry(r.Context(), b)
		if err != nil {
			if errors.Is(err, chunker.ErrNotFound) {
				log.Warnw("No Content Found", "CID", b.String())
				http.Error(w, "", http.StatusNotFound)
				return
			}
			log.Errorf("failed to get entry %s for peer %s: %v", b.String(), providerID, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/cbor")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(entry)
		if err != nil {
			log.Errorw("failed to write HTTP response", "err", err)
		}
		return
	default:
		// In case IPNI did not provide the requested header
		ad, err := p.getAdBytes(r.Context(), b, providerID)
		if err != nil {
			if errors.Is(err, chunker.ErrNotFound) {
				// Check if this is an entry CID
				entry, err := p.sc.GetEntry(r.Context(), b)
				if err != nil {
					if errors.Is(err, chunker.ErrNotFound) {
						log.Warnw("No Content Found", "CID", b.String())
						http.Error(w, "", http.StatusNotFound)
						return
					}
					content = "entry"
					log.Errorf("failed to get entry %s for peer %s: %v", b.String(), providerID, err)
					http.Error(w, "", http.StatusInternalServerError)
					return
				}
				content = "entry"
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/cbor")
				_, err = w.Write(entry)
				if err != nil {
					log.Errorw("failed to write HTTP response", "err", err)
				}
				return
			}
			content = "advertisement"
			log.Errorf("failed to get ad %s for peer %s: %v", b.String(), providerID, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		content = "advertisement"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(ad)
		if err != nil {
			log.Errorw("failed to write HTTP response", "err", err)
		}
		return
	}
}

// Routes sets up the routes for the IPNI provider.
// It registers a handler function for the GET request at the IPNIRoutePath.
// The handler function is provided by the Provider struct.
func Routes(r *chi.Mux, p *Provider) {
	// /ipni-provider/{providerId}/ipni/v1/ad/head
	r.Get(IPNIRoutePath+"{providerId}"+IPNIPath+"head", p.handleGetHead)

	// /ipni-provider/{providerId}/ipni/v1/ad/{cid}
	r.Get(IPNIRoutePath+"{providerId}"+IPNIPath+"{cid}", p.handleGet)
}

const (
	CIDContactFilter = "cid.contact"
)

func FilterAnnounceURLs(slice []*url.URL, filter []string) []*url.URL {

	return lo.Filter(slice, func(item *url.URL, index int) bool {
		for _, f := range filter {
			if strings.Contains(item.String(), f) {
				return false
			}
		}
		return true
	})
}

func (p *Provider) StartPublishing(ctx context.Context) {
	p.startLock.Do(func() {
		go p.startPublishing(ctx)
	})
}

// StartPublishing starts a poller which publishes the head for each provider every 10 minutes.
func (p *Provider) startPublishing(ctx context.Context) {

	// Remove cid.contact from announceURLs if the build is debug or testnet
	if build.BuildType == build.Build2k || build.BuildType == build.BuildDebug {
		urls := FilterAnnounceURLs(p.announceURLs, []string{CIDContactFilter})
		if len(urls) == 0 {
			log.Warn("Not starting IPNI provider publishing as there are no other URLs except cid.contact for testnet build")
			return
		}
		p.announceURLs = urls
	}

	ticker := time.NewTicker(PublishInterval)

	for {
		select {
		case <-ticker.C:
			if err := p.refreshProviders(ctx); err != nil {
				log.Errorw("failed to refresh ipni peers", "err", err)
			}
			// Call the function to publish head for each provider
			p.publishHead(ctx)
			p.maybeUpdateSparkContract(ctx)
		case <-ctx.Done():
			ticker.Stop()
			return
		}
	}
}

// getHeadCID queries the database to retrieve the head CID for a specific provider.
// If the head CID is not found or an error occurs, it returns cid.Undef and the error respectively.
func (p *Provider) getHeadCID(ctx context.Context, provider string) (cid.Cid, error) {
	var headStr string
	err := p.db.QueryRow(ctx, `SELECT head FROM ipni_head WHERE provider = $1`, provider).Scan(&headStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cid.Undef, nil
		}
		return cid.Undef, xerrors.Errorf("querying previous head: %w", err)
	}

	if headStr == "" {
		return cid.Undef, chunker.ErrNotFound
	}

	return cid.Parse(headStr)
}

// publishHead iterates over each provider's keys and publishes the head CID for that provider.
// It calls the getHeadCID method to retrieve the head CID for each provider. If an error occurs, it logs the error and continues to the next provider.
// It then calls the publishhttp method to publish the head CID via HTTP. If an error occurs, it logs the error.
// The function is intended to be run as a goroutine with a ticker to schedule its execution at regular intervals.
func (p *Provider) publishHead(ctx context.Context) {
	var i int
	p.mu.RLock()
	providers := make([]string, 0, len(p.providerInfos))
	for provider := range p.providerInfos {
		providers = append(providers, provider)
	}
	p.mu.RUnlock()

	for _, provider := range providers {
		if i > 0 {
			time.Sleep(interProviderAnnounceDelay)
		}
		c, err := p.getHeadCID(ctx, provider)
		if err != nil {
			log.Errorw("failed to get head CID", "provider", provider, "error", err)
			continue
		}
		if c == cid.Undef {
			log.Debugw("No head CID found for provider", "provider", provider)
			continue
		}

		if _, ok := p.latest[provider]; ok && p.latest[provider] == c {
			log.Debugw("Skipping duplicate announce for provider", "provider", provider, "cid", c.String())
			continue
		}

		log.Infow("Publishing head for provider", "provider", provider, "cid", c.String())
		err = p.publishhttp(ctx, c, provider)
		if err != nil {
			recordAnnounceAttempt(provider, "error")
			log.Errorw("failed to publish head for provide", "provider", provider, "error", err)
		} else {
			recordAnnounceAttempt(provider, "success")
			p.latest[provider] = c
			p.mu.Lock()
			info, ok := p.providerInfos[provider]
			if !ok {
				log.Warnw("cannot update provider announce times", "provider", provider, "error", "provider not found")
			} else {
				info.lastPublishTime = new(time.Now())
			}
			p.mu.Unlock()
		}

		i++
	}
}

// publishhttp sends an HTTP announce message for the given advertisement CID and peer ID.
// It creates an HTTP announce sender using the provided announce URLs and the private key of the peer.
// It obtains the HTTP addresses for the peer and sends the announce message to those addresses.
func (p *Provider) publishhttp(ctx context.Context, adCid cid.Cid, peer string) error {
	// Create the http announce sender.
	log.Infow("Creating http announce sender", "urls", p.announceURLs)

	lrt := &loggingRoundTripper{
		proxied: http.DefaultTransport,
		peer:    peer,
		adCid:   adCid,
	}

	c := &http.Client{
		Transport: lrt,
		Timeout:   1 * time.Minute,
	}

	// Don't announce PoRep Ads to Filecoin Pin indexer
	a := p.announceURLs

	p.mu.RLock()
	info, infoOk := p.providerInfos[peer]
	p.mu.RUnlock()

	if !infoOk {
		return fmt.Errorf("no details found for peer %s", peer)
	}

	httpSender, err := httpsender.New(a, info.ID, httpsender.WithClient(c))
	if err != nil {
		return fmt.Errorf("cannot create http announce sender: %w", err)
	}

	addrs := []multiaddr.Multiaddr{info.httpServerAddresses}

	log.Infow("Announcing advertisements over HTTP", "urls", p.announceURLs)
	return announce.Send(ctx, adCid, addrs, httpSender)
}

type loggingRoundTripper struct {
	proxied http.RoundTripper
	peer    string
	adCid   cid.Cid
}

func (lrt *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	log.Infow("Announcing IPNI advertisement", "provider", lrt.peer, "cid", lrt.adCid, "url", req.URL.String())

	start := time.Now()
	res, err := lrt.proxied.RoundTrip(req)
	if err != nil {
		observeAnnounceHTTPRoundTrip(lrt.peer, "error", time.Since(start))
		log.Warnw("IPNI announcement request failed", "provider", lrt.peer, "cid", lrt.adCid, "url", req.URL.String(), "err", err)
		return nil, err
	}
	observeAnnounceHTTPRoundTrip(lrt.peer, fmt.Sprintf("%d", res.StatusCode), time.Since(start))

	log.Infow("IPNI announcement response", "provider", lrt.peer, "cid", lrt.adCid, "url", req.URL.String(), "status", res.StatusCode)
	return res, nil
}

func (p *Provider) LastPublishTime(providerPeerID string) *time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	info, ok := p.providerInfos[providerPeerID]
	if !ok {
		return nil
	}
	return info.lastPublishTime
}

// adSyncStatus mirrors storetheindex's GET /sync/status/ad/{adCid} response (ipni/storetheindex#2898).
type adSyncStatus struct {
	Ad          string
	Indexed     bool
	IndexedTime *time.Time
	Skipped     bool
	SkippedTime *time.Time
}

// SyncedAt returns the cached indexed time for adCid, or nil on a cache miss.
func (p *Provider) SyncedAt(adCid cid.Cid, provider string) *time.Time {
	if indexedAt, ok := p.syncCache.Get(adCid); ok {
		return indexedAt
	}
	p.checkAdSyncStatusAsync(adCid, provider)
	return nil
}

// checkAdSyncStatusAsync queries the indexer services for adCid in the background.
func (p *Provider) checkAdSyncStatusAsync(adCid cid.Cid, provider string) {
	if _, alreadyChecking := p.syncChecksInFlight.LoadOrStore(adCid, struct{}{}); alreadyChecking {
		return
	}

	select {
	case p.syncCheckSem <- struct{}{}:
	default:
		p.syncChecksInFlight.Delete(adCid)
		return
	}

	go func() {
		defer p.syncChecksInFlight.Delete(adCid)
		defer func() { <-p.syncCheckSem }()

		ctx := context.Background()
		checkStart := time.Now()

		service, status, ok := p.queryAdSyncStatus(ctx, adCid)
		if !ok {
			return // still pending
		}
		checkTook := time.Since(checkStart)

		switch {
		case status.Indexed:
			indexedAt := time.Now().UTC() // fallback when IndexedTime is nil
			var latency *time.Duration
			if status.IndexedTime != nil {
				indexedAt = status.IndexedTime.UTC()
				// Only a real, indexer-reported IndexedTime is a trustworthy
				latency = p.observeSyncLatency(ctx, adCid, provider, indexedAt)
			}
			p.syncCache.Add(adCid, &indexedAt)
			log.Infow("IPNI advertisement confirmed indexed", "provider", provider, "ad_cid", adCid.String(), "service", service, "indexed_at", indexedAt, "check_took", checkTook, "latency", latency)
		case status.Skipped:
			log.Warnw("indexer permanently skipped ad, it will never be indexed", "ad_cid", adCid.String(), "service", service, "check_took", checkTook)
			p.syncCache.Add(adCid, nil)
		}
	}()
}

// queryAdSyncStatus checks each configured service in turn for a definitive answer.
func (p *Provider) queryAdSyncStatus(ctx context.Context, adCid cid.Cid) (service string, status adSyncStatus, ok bool) {
	for _, svc := range p.serviceURLs {
		u := *svc
		u.Path = path.Join(u.Path, "sync", "status", "ad", adCid.String())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			log.Warnw("failed to build ad sync status request", "service", svc.String(), "ad_cid", adCid.String(), "err", err)
			continue
		}

		resp, err := p.syncClient.Do(req)
		if err != nil {
			log.Debugw("failed to query ad sync status", "service", svc.String(), "ad_cid", adCid.String(), "err", err)
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			log.Debugw("indexer service does not support ad sync status endpoint, skipping", "service", svc.String())
			_ = resp.Body.Close()
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Debugw("unexpected ad sync status response", "service", svc.String(), "ad_cid", adCid.String(), "status", resp.StatusCode)
			_ = resp.Body.Close()
			continue
		}

		var s adSyncStatus
		err = json.NewDecoder(resp.Body).Decode(&s)
		_ = resp.Body.Close()
		if err != nil {
			log.Warnw("failed to decode ad sync status response", "service", svc.String(), "ad_cid", adCid.String(), "err", err)
			continue
		}

		if s.Indexed || s.Skipped {
			return svc.String(), s, true
		}
	}
	return "", adSyncStatus{}, false
}

// observeSyncLatency records the sync-latency metric, estimating announce
// time as ipni.created_at + PublishInterval. Returns nil if latency can't be computed.
func (p *Provider) observeSyncLatency(ctx context.Context, adCid cid.Cid, provider string, indexedAt time.Time) *time.Duration {
	var createdAt sql.NullTime
	err := p.db.QueryRow(ctx, `SELECT created_at FROM ipni WHERE ad_cid = $1`, adCid.String()).Scan(&createdAt)
	if err != nil || !createdAt.Valid {
		log.Debugw("no ipni.created_at to compute sync latency", "ad_cid", adCid.String(), "err", err)
		return nil
	}

	announcedAt := createdAt.Time.Add(PublishInterval)
	if indexedAt.Before(announcedAt) {
		return nil // avoid a negative duration
	}

	latency := indexedAt.Sub(announcedAt)
	observeIndexedLatency(provider, latency)
	return &latency
}
