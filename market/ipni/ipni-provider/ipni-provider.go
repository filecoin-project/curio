package ipni_provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
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
	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
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

const MaxCachedReaders = 50

// validate is a boolean variable that determines whether to validate the reconstructed chunk node against the expected chunk CID.
// If validate is true, the chunk node is validated against the expected chunk CID.
// If the chunk node does not match the expected chunk CID, an error is returned.
// If validate is false, the chunk node is not validated.
var validate = true

// log is a logger instance initialized with the name "ipni-provider".
// ErrNotFound is an error variable initialized with the value "not found".
var (
	log         = logging.Logger("ipni-provider")
	ErrNotFound = errors.New("not found")
)

// peerInfo represents information about a peer, including its ID and private key.
type peerInfo struct {
	ID  peer.ID
	Key crypto.PrivKey
}

// Provider represents a provider for IPNI.
type Provider struct {
	db            *harmonydb.DB
	pieceProvider *pieceprovider.PieceProvider
	indexStore    *indexstore.IndexStore
	keys          map[string]*peerInfo // map[peerID String]Private_Key
	// announceURLs enables sending direct announcements via HTTP. This is
	// the list of indexer URLs to send direct HTTP announce messages to.
	announceURLs []*url.URL
	// httpServerAddresses has a list of all the addresses where IPNI can reach to sync with
	// the provider. This is created by converting announceURLs into a multiaddr and adding the following
	// Curio HTTP URL(in multiaddr)+IPNIRoutePath(/ipni-provider/)+peerID
	httpServerAddresses []multiaddr.Multiaddr
	cpr                 *cachedreader.CachedPieceReader
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

	rows, err := d.DB.Query(ctx, `SELECT priv_key FROM ipni_peerid`)
	if err != nil {
		return nil, xerrors.Errorf("failed to get private libp2p keys from DB: %w", err)
	}

	defer rows.Close()

	for rows.Next() && rows.Err() == nil {
		var priv []byte
		err := rows.Scan(&priv)
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

		keyMap[id.String()] = &peerInfo{
			Key: pkey,
			ID:  id,
		}
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

	httpServerAddresses := make([]multiaddr.Multiaddr, 0, len(d.Cfg.Market.StorageMarketConfig.IPNI.AnnounceAddresses))

	for _, a := range d.Cfg.Market.StorageMarketConfig.IPNI.AnnounceAddresses {
		u, err := url.Parse(strings.TrimSpace(a))
		if err != nil {
			return nil, xerrors.Errorf("parsing announce address: %w", err)
		}
		u.Path = path.Join(u.Path, IPNIRoutePath)
		addr, err := maurl.FromURL(u)
		if err != nil {
			return nil, xerrors.Errorf("converting URL to multiaddr: %w", err)
		}
		httpServerAddresses = append(httpServerAddresses, addr)

		log.Infow("Announce address", "address", addr.String(), "url", u.String())
	}

	return &Provider{
		db:                  d.DB,
		pieceProvider:       d.PieceProvider,
		indexStore:          d.IndexStore,
		keys:                keyMap,
		announceURLs:        announceURLs,
		httpServerAddresses: httpServerAddresses,
		cpr:                 d.CachedPieceReader,
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
		return schema.Advertisement{}, ErrNotFound
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
		strings.Split(a.Addresses, "|")
	}

	if a.Previous != nil {
		prev, err := cid.Parse(a.Previous)
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
		return nil, ErrNotFound
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

// getEntry retrieves an entry from the provider's database based on the given block CID and provider ID.
// It returns the entry data as a byte slice, or an error if the entry is not found or an error occurs during retrieval.
// If the entry is stored as a CAR file, it reconstructs the chunk from the CAR file.
func (p *Provider) getEntry(block cid.Cid) ([]byte, error) {
	// We should use background context to avoid early exit
	// while chunking as first attempt will always fail
	ctx := context.Background()

	type ipniChunk struct {
		PieceCID string `db:"piece_cid"`
		FromCar  bool   `db:"from_car"`

		FirstCID    *string `db:"first_cid"`
		StartOffset *int64  `db:"start_offset"`
		NumBlocks   int64   `db:"num_blocks"`

		PrevCID *string `db:"prev_cid"`
	}

	var ipniChunks []ipniChunk

	err := p.db.Select(ctx, &ipniChunks, `SELECT 
			current.piece_cid, 
			current.from_car, 
			current.first_cid, 
			current.start_offset, 
			current.num_blocks, 
			prev.cid AS prev_cid
		FROM 
			ipni_chunks current
		LEFT JOIN 
			ipni_chunks prev 
		ON 
			current.piece_cid = prev.piece_cid AND
			current.chunk_num = prev.chunk_num + 1
		WHERE 
			current.cid = $1
		LIMIT 1;`, block.String())
	if err != nil {
		return nil, xerrors.Errorf("querying chunks with entry link %s: %w", block, err)
	}

	if len(ipniChunks) == 0 {
		return nil, ErrNotFound
	}

	chunk := ipniChunks[0]

	pieceCid, err := cid.Parse(chunk.PieceCID)
	if err != nil {
		return nil, xerrors.Errorf("parsing piece CID: %w", err)
	}

	if !chunk.FromCar {
		if chunk.FirstCID == nil {
			return nil, xerrors.Errorf("chunk does not have first CID")
		}

		cb, err := hex.DecodeString(*chunk.FirstCID)
		if err != nil {
			return nil, xerrors.Errorf("decoding first CID: %w", err)
		}

		firstHash := multihash.Multihash(cb)

		var next ipld.Link
		if chunk.PrevCID != nil {
			prevChunk, err := cid.Parse(*chunk.PrevCID)
			if err != nil {
				return nil, xerrors.Errorf("parsing previous CID: %w", err)
			}

			next = cidlink.Link{Cid: prevChunk}
		}

		return p.reconstructChunkFromDB(ctx, block, pieceCid, firstHash, next, chunk.NumBlocks)
	}

	return p.reconstructChunkFromCar(ctx, block, pieceCid, *chunk.StartOffset, nil, chunk.NumBlocks)
}

// reconstructChunkFromCar reconstructs a chunk from a car file.
func (p *Provider) reconstructChunkFromCar(ctx context.Context, chunk, piece cid.Cid, startOff int64, next ipld.Link, numBlocks int64) ([]byte, error) {

	reader, _, err := p.cpr.GetSharedPieceReader(ctx, piece)
	defer func(reader storiface.Reader) {
		_ = reader.Close()
	}(reader)

	if err != nil {
		return nil, xerrors.Errorf("failed to read piece %s for ipni chunk %s reconstruction: %w", piece, chunk, err)
	}

	_, err = reader.Seek(startOff, io.SeekStart)
	if err != nil {
		return nil, xerrors.Errorf("seeking to start offset: %w", err)
	}

	br := bufio.NewReader(reader)

	mhs := make([]multihash.Multihash, 0, numBlocks)
	for i := int64(0); i < numBlocks; i++ {
		bcid, err := ipniculib.SkipCarNode(br)
		if err != nil {
			return nil, xerrors.Errorf("skipping car node: %w", err)
		}

		mhs = append(mhs, bcid.Hash())
	}

	// Create the chunk node
	chunkNode, err := chunker.NewEntriesChunkNode(mhs, next)
	if err != nil {
		return nil, xerrors.Errorf("creating chunk node: %w", err)
	}

	if validate {
		link, err := ipniculib.NodeToLink(chunkNode, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		if link.String() != chunk.String() {
			return nil, xerrors.Errorf("chunk node does not match the expected chunk CID, got %s, expected %s", link.String(), chunk.String())
		}
	}

	b := new(bytes.Buffer)
	err = dagcbor.Encode(chunkNode, b)
	if err != nil {
		return nil, xerrors.Errorf("encoding chunk node: %w", err)
	}

	return b.Bytes(), nil
}

// ReconstructChunkFromDB reconstructs a chunk from the database.
func (p *Provider) reconstructChunkFromDB(ctx context.Context, chunk, piece cid.Cid, firstHash multihash.Multihash, next ipld.Link, numBlocks int64) ([]byte, error) {
	mhs, err := p.indexStore.GetPieceHashRange(ctx, piece, firstHash, numBlocks)
	if err != nil {
		return nil, xerrors.Errorf("getting piece hash range: %w", err)
	}

	// Create the chunk node
	chunkNode, err := chunker.NewEntriesChunkNode(mhs, next)
	if err != nil {
		return nil, xerrors.Errorf("creating chunk node: %w", err)
	}

	if validate {
		link, err := ipniculib.NodeToLink(chunkNode, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		if link.String() != chunk.String() {
			return nil, xerrors.Errorf("chunk node does not match the expected chunk CID, got %s, expected %s", link.String(), chunk.String())
		}
	}

	b := new(bytes.Buffer)
	err = dagcbor.Encode(chunkNode, b)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func (p *Provider) handleGetHead(w http.ResponseWriter, r *http.Request) {
	log.Infow("Received IPNI request", "path", r.URL.Path)

	providerID := chi.URLParam(r, "providerId")
	sh, err := p.getHead(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
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

	log.Infow("Received IPNI request", "path", r.URL.Path, "cid", reqCid, "providerId", providerID)

	b, err := cid.Parse(reqCid)
	if err != nil {
		log.Debugw("invalid CID as path parameter while getting content", "request", reqCid, "err", err)
		http.Error(w, "invalid CID: "+reqCid, http.StatusBadRequest)
		return
	}

	h := r.Header.Get(ipnisync.CidSchemaHeader)

	switch h {
	case ipnisync.CidSchemaAdvertisement:
		ad, err := p.getAdBytes(r.Context(), b, providerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
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
		entry, err := p.getEntry(b)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				log.Debugw("No Content Found", "CID", b.String())
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
			if errors.Is(err, ErrNotFound) {
				// Check if this is an entry CID
				entry, err := p.getEntry(b)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						log.Debugw("No Content Found", "CID", b.String())
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
	// A poller which publishes head for each provider
	// every 10 minutes
	ticker := time.NewTicker(publishInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				// Call the function to publish head for each provider
				p.publishHead(ctx)
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
		return cid.Undef, ErrNotFound
	}

	return cid.Parse(headStr)
}

// publishHead iterates over each provider's keys and publishes the head CID for that provider.
// It calls the getHeadCID method to retrieve the head CID for each provider. If an error occurs, it logs the error and continues to the next provider.
// It then calls the publishhttp method to publish the head CID via HTTP. If an error occurs, it logs the error.
// The function is intended to be run as a goroutine with a ticker to schedule its execution at regular intervals.
func (p *Provider) publishHead(ctx context.Context) {
	for provider := range p.keys {
		c, err := p.getHeadCID(ctx, provider)
		if err != nil {
			log.Errorw("failed to get head CID", "provider", provider, "error", err)
			continue
		}
		err = p.publishhttp(ctx, c, provider)
		if err != nil {
			log.Errorw("failed to publish head for provide", "provider", provider, "error", err)
		}
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
	for _, addr := range p.httpServerAddresses {
		u, err := maurl.ToURL(addr)
		if err != nil {
			return nil, err
		}

		u.Path = path.Join(u.Path, peer)

		ma, err := maurl.FromURL(u)
		if err != nil {
			return nil, xerrors.Errorf("converting URL to multiaddr: %w", err)
		}

		ret = append(ret, ma)
	}

	log.Infow("HTTP addresses for peer", "peer", peer, "addresses", ret)

	return ret, nil
}
