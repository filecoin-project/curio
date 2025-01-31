package ipni_provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/filecoin-project/curio/build"
	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce"
	"github.com/ipni/go-libipni/announce/httpsender"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/maurl"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

// IPNIRoutePath is a constant representing the route path for IPNI provider.
const IPNIRoutePath = "/ipni-provider/"

// IPNIPath is a constant that represents the path for IPNI API requests.
const IPNIPath = "/ipni/v1/ad/"

// publishInterval represents the time interval between each publishing operation.
// It is set to 10 minutes.
const publishInterval = 10 * time.Minute
const publishProviderSpacing = 5 * time.Minute

var (
	log = logging.Logger("ipni-provider")
)

// peerInfo represents information about a peer, including its ID and private key.
type peerInfo struct {
	ID    peer.ID
	Key   crypto.PrivKey
	SPID  abi.ActorID
	Miner address.Address
}

// Provider represents a provider for IPNI.
type Provider struct {
	full          api.Chain
	db            *harmonydb.DB
	pieceProvider *pieceprovider.PieceProvider
	indexStore    *indexstore.IndexStore
	sc            *chunker.ServeChunker
	keys          map[string]*peerInfo // map[peerID String]Private_Key
	// announceURLs enables sending direct announcements via HTTP. This is
	// the list of indexer URLs to send direct HTTP announce messages to.
	announceURLs []*url.URL
	// httpServerAddresses has a list of all the addresses where IPNI can reach to sync with
	// the provider. This is created by converting announceURLs into a multiaddr and adding the following
	// Curio HTTP URL(in multiaddr)+IPNIRoutePath(/ipni-provider/)+peerID
	httpServerAddresses map[string]multiaddr.Multiaddr // map[peerID String]Multiaddr
}

// NewProvider initializes a new Provider using the provided dependencies.
// It retrieves private libp2p keys from the database, unmarshals them, generates peer IDs,
// and populates a keyMap with the corresponding peer information.
// It also sets up the announce URLs and HTTP server addresses based on the configuration.
// The Provider struct is then created with the populated fields and returned along with nil error.
// If any error occurs during the process, it is returned along with a non-nil Provider.
func NewProvider(d *deps.Deps) (*Provider, error) {
	ctx := context.Background()

	keyMap := make(map[string]*peerInfo)

	rows, err := d.DB.Query(ctx, `SELECT priv_key, peer_id, sp_id FROM ipni_peerid`)
	if err != nil {
		return nil, xerrors.Errorf("failed to get private libp2p keys from DB: %w", err)
	}

	defer rows.Close()

	for rows.Next() && rows.Err() == nil {
		var priv []byte
		var peerID string
		var spID abi.ActorID
		err := rows.Scan(&priv, &peerID, &spID)
		if err != nil {
			return nil, xerrors.Errorf("failed to scan the row: %w", err)
		}

		pkey, err := crypto.UnmarshalPrivateKey(priv)
		if err != nil {
			return nil, xerrors.Errorf("unmarshaling private key: %w", err)
		}

		id, err := peer.IDFromPublicKey(pkey.GetPublic())
		if err != nil {
			return nil, xerrors.Errorf("generating peer ID from private key: %w", err)
		}

		if id.String() != peerID {
			return nil, xerrors.Errorf("peer ID mismatch: got %s (calculated), expected %s (DB)", id.String(), peerID)
		}

		maddr, err := address.NewIDAddress(uint64(spID))
		if err != nil {
			return nil, xerrors.Errorf("parsing miner ID: %w", err)
		}

		keyMap[id.String()] = &peerInfo{
			Key:   pkey,
			ID:    id,
			SPID:  spID,
			Miner: maddr,
		}

		log.Infow("ipni peer ID", "peerID", id.String())
	}

	if rows.Err() != nil {
		return nil, err
	}

	announceURLs := make([]*url.URL, len(d.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs))

	for i, us := range d.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs {
		u, err := url.Parse(us)
		if err != nil {
			return nil, err
		}
		announceURLs[i] = u
	}

	httpServerAddresses := map[string]multiaddr.Multiaddr{}

	{
		u, err := url.Parse(fmt.Sprintf("https://%s", d.Cfg.HTTP.DomainName))
		if err != nil {
			return nil, xerrors.Errorf("parsing announce address domain: %w", err)
		}
		u.Path = path.Join(u.Path, IPNIRoutePath)

		for pid := range keyMap {
			u := *u
			u.Path = path.Join(u.Path, pid)
			addr, err := maurl.FromURL(&u)
			if err != nil {
				return nil, xerrors.Errorf("converting URL to multiaddr: %w", err)
			}

			httpServerAddresses[pid] = addr

			log.Infow("Announce address", "address", addr.String(), "pid", pid, "url", u.String())
		}
	}

	return &Provider{
		full:                d.Chain,
		db:                  d.DB,
		pieceProvider:       d.PieceProvider,
		indexStore:          d.IndexStore,
		sc:                  d.ServeChunker,
		keys:                keyMap,
		announceURLs:        announceURLs,
		httpServerAddresses: httpServerAddresses,
	}, nil
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
	}

	err := p.db.Select(ctx, &ads, `SELECT 
										context_id,
										is_rm, 
										previous, 
										provider, 
										addresses, 
										signature, 
										entries 
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

	mds := metadata.IpfsGatewayHttp{}
	md, err := mds.MarshalBinary()
	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("marshalling metadata: %w", err)
	}

	adv := schema.Advertisement{
		Provider:  a.Provider,
		Signature: a.Signature,
		Entries:   cidlink.Link{Cid: e},
		ContextID: a.ContextID,
		IsRm:      a.IsRm,
		Metadata:  md,
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
		return nil, xerrors.Errorf("querying previous head: %w", err)
	}

	if headStr == "" {
		return nil, chunker.ErrNotFound
	}

	ad, err := cid.Parse(headStr)
	if err != nil {
		return nil, xerrors.Errorf("parsing head CID: %w", err)
	}

	signedHead, err := head.NewSignedHead(ad, "", p.keys[provider].Key)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate signed head for peer %s: %w", provider, err)
	}

	return signedHead.Encode()
}

func (p *Provider) handleGetHead(w http.ResponseWriter, r *http.Request) {
	log.Infow("Received IPNI request", "path", r.URL.Path)

	providerID := chi.URLParam(r, "providerId")
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

	defer func() {
		log.Infow("Served IPNI request", "path", r.URL.Path, "cid", reqCid, "providerId", providerID, "took", time.Since(start))
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
					log.Errorf("failed to get entry %s for peer %s: %v", b.String(), providerID, err)
					http.Error(w, "", http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/cbor")
				_, err = w.Write(entry)
				if err != nil {
					log.Errorw("failed to write HTTP response", "err", err)
				}
				return
			}
			log.Errorf("failed to get ad %s for peer %s: %v", b.String(), providerID, err)
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

// StartPublishing starts a poller which publishes the head for each provider every 10 minutes.
func (p *Provider) StartPublishing(ctx context.Context) {
	// Do not publish for any network except mainnet
	if build.BuildType != build.BuildMainnet {
		return
	}

	// A poller which publishes head for each provider
	// every 10 minutes
	ticker := time.NewTicker(publishInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				// Call the function to publish head for each provider
				p.publishHead(ctx)
				err := p.updateSparkContract(ctx)
				if err != nil {
					log.Errorw("failed to update ipni provider peer mapping", "err", err)
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

// getHeadCID queries the database to retrieve the head CID for a specific provider.
// If the head CID is not found or an error occurs, it returns cid.Undef and the error respectively.
func (p *Provider) getHeadCID(ctx context.Context, provider string) (cid.Cid, error) {
	var headStr string
	err := p.db.QueryRow(ctx, `SELECT head FROM ipni_head WHERE provider = $1`, provider).Scan(&headStr)
	if err != nil {
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
	for provider := range p.keys {
		if i > 0 {
			time.Sleep(publishProviderSpacing)
		}
		c, err := p.getHeadCID(ctx, provider)
		if err != nil {
			log.Errorw("failed to get head CID", "provider", provider, "error", err)
			continue
		}
		err = p.publishhttp(ctx, c, provider)
		if err != nil {
			log.Errorw("failed to publish head for provide", "provider", provider, "error", err)
		}

		i++
	}
}

// publishhttp sends an HTTP announce message for the given advertisement CID and peer ID.
// It creates an HTTP announce sender using the provided announce URLs and the private key of the peer.
// It obtains the HTTP addresses for the peer and sends the announce message to those addresses.
func (p *Provider) publishhttp(ctx context.Context, adCid cid.Cid, peer string) error {
	// Create the http announce sender.
	httpSender, err := httpsender.New(p.announceURLs, p.keys[peer].ID)
	if err != nil {
		return fmt.Errorf("cannot create http announce sender: %w", err)
	}

	addrs, err := p.getHTTPAddressForPeer(peer)
	if err != nil {
		return fmt.Errorf("cannot create provider http addresses: %w", err)
	}

	log.Infow("Announcing advertisements over HTTP", "urls", p.announceURLs)
	return announce.Send(ctx, adCid, addrs, httpSender)
}

// getHTTPAddressForPeer returns the HTTP addresses for a given peer.
func (p *Provider) getHTTPAddressForPeer(peer string) ([]multiaddr.Multiaddr, error) {
	var ret []multiaddr.Multiaddr

	r, ok := p.httpServerAddresses[peer]
	if !ok {
		log.Errorw("no HTTP address for peer", "peer", peer)
		return nil, fmt.Errorf("no HTTP address for peer %s", peer)
	}

	ret = append(ret, r)

	log.Infow("HTTP addresses for peer", "peer", peer, "addresses", ret)

	return ret, nil
}
