package ipni_provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/index"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagjson"
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce"
	"github.com/ipni/go-libipni/announce/httpsender"
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ipni/chunker"
	"github.com/filecoin-project/curio/lib/ipni/ipniculib"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/urltomultiaddr"

	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const IPNIRoutePath = "/ipni-provider"
const IPNIPath = "/ipni/v1/ad/"
const ProviderPath = "/ipni-provider"

var (
	log         = logging.Logger("ipni-provider")
	ErrNotFound = errors.New("not found")
)

type ipniAPI interface {
	StateNetworkName(context.Context) (dtypes.NetworkName, error)
}

type peerInfo struct {
	ID  peer.ID
	Key crypto.PrivKey
}

type Provider struct {
	api            ipniAPI
	db             *harmonydb.DB
	pieceProvider  *pieceprovider.PieceProvider
	entriesChunker *chunker.Chunker
	cache          *lru.Cache[ipld.Link, datamodel.Node]
	topic          string
	httpPrefix     string
	keys           map[string]*peerInfo // map[peerID String]Private_Key
	// announceURLs enables sending direct announcements via HTTP. This is
	// the list of indexer URLs to send direct HTTP announce messages to.
	announceURLs        []*url.URL
	httpServerAddresses []multiaddr.Multiaddr
}

func NewProvider(api ipniAPI, deps *deps.Deps) (*Provider, error) {
	c, err := lru.New[ipld.Link, datamodel.Node](chunker.EntriesCacheCapacity)
	if err != nil {
		return nil, xerrors.Errorf("creating new cache: %w", err)
	}

	ctx := context.Background()

	keyMap := make(map[string]*peerInfo)

	rows, err := deps.DB.Query(ctx, `SELECT priv_key FROM libp2p`)
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

	nn, err := api.StateNetworkName(ctx)
	if err != nil {
		return nil, err
	}

	topic := "/indexer/ingest/" + string(nn)

	if deps.Cfg.Market.StorageMarketConfig.IPNI.TopicName != "" {
		topic = deps.Cfg.Market.StorageMarketConfig.IPNI.TopicName
	}

	var announceURLs []*url.URL

	for _, us := range deps.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs {
		u, err := url.Parse(us)
		if err != nil {
			return nil, err
		}
		announceURLs = append(announceURLs, u)
	}

	var httpServerAddresses []multiaddr.Multiaddr

	for _, a := range deps.Cfg.Market.HTTP.AnnounceAddresses {
		addr, err := urltomultiaddr.UrlToMultiaddr(a)
		if err != nil {
			return nil, err
		}
		addr, err = multiaddr.NewMultiaddr(addr.String() + IPNIRoutePath)
		if err != nil {
			return nil, err
		}
		httpServerAddresses = append(httpServerAddresses, addr)
	}

	return &Provider{
		api:                 api,
		db:                  deps.DB,
		pieceProvider:       deps.PieceProvider,
		cache:               c,
		keys:                keyMap,
		topic:               topic,
		announceURLs:        announceURLs,
		httpServerAddresses: httpServerAddresses,
	}, nil
}

func (p *Provider) getAd(ctx context.Context, ad cid.Cid, provider string) (schema.Advertisement, error) {
	var ads []struct {
		PreviousID string
		Provider   string
		Addresses  string
		Signature  []byte
		Entries    string
		ContextID  []byte
		IsRm       bool
	}

	err := p.db.Select(ctx, &ads, `SELECT context_id,
       is_rm, previous, provider, addresses, 
       signature, entries FROM ipni WHERE ad_cid = $1 AND provider = $2`, ad.String(), provider)

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

	prev, err := cid.Parse(a.PreviousID)
	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("parsing previous CID: %w", err)
	}

	e, err := cid.Parse(a.Entries)
	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("parsing entry CID: %w", err)
	}

	mds := metadata.IpfsGatewayHttp{}
	md, err := mds.MarshalBinary()
	if err != nil {
		return schema.Advertisement{}, xerrors.Errorf("marshalling metadata: %w", err)
	}

	return schema.Advertisement{
		PreviousID: cidlink.Link{Cid: prev},
		Provider:   a.Provider,
		Addresses:  strings.Split(a.Addresses, ","),
		Signature:  a.Signature,
		Entries:    cidlink.Link{Cid: e},
		ContextID:  a.ContextID,
		IsRm:       a.IsRm,
		Metadata:   md,
	}, nil
}

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
		return nil, err
	}

	h, err := p.getAd(ctx, ad, provider)
	if err != nil {
		return nil, err
	}

	hn, err := h.ToNode()
	if err != nil {
		return nil, err
	}

	lnk, err := ipniculib.NodeToLink(hn, schema.Linkproto)
	if err != nil {
		return nil, err
	}

	signedHead, err := head.NewSignedHead(lnk.(cidlink.Link).Cid, p.topic, p.keys[provider].Key)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate signed head for peer %s: %w", provider, err)
	}

	return signedHead.Encode()
}

func (p *Provider) GetEntry(block cid.Cid, provider string) ([]byte, error) {
	// We should use background context to avoid early exit
	// while chunking as first attempt will always fail
	ctx := context.Background()

	// Check if cache has it
	cacheKey := cidlink.Link{Cid: block}
	if value, ok := p.cache.Get(cacheKey); ok {
		b := new(bytes.Buffer)
		err := dagjson.Encode(value, b)
		if err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}

	// If cache does not have it then this must be Entry Head
	// We should find the relevant ContextID(abi.PieceInfo) and chunk the piece again
	// to generate all the required links

	var pis []abi.PieceInfo

	rows, err := p.db.Query(ctx, `SELECT context_id FROM ipni WHERE entries = $1 AND provider = $2`, block.String(), provider)
	if err != nil {
		return nil, xerrors.Errorf("querying ads with entry link %s: %w", block, err)
	}

	defer rows.Close()

	for rows.Next() && rows.Err() == nil {
		var contextID []byte
		err := rows.Scan(&contextID)
		if err != nil {
			return nil, xerrors.Errorf("failed to scan the row: %w", err)
		}

		var pi abi.PieceInfo
		err = pi.UnmarshalCBOR(bytes.NewReader(contextID))
		if err != nil {
			return nil, xerrors.Errorf("unmarshaling piece info: %w", err)
		}

		pis = append(pis, pi)
	}

	if rows.Err() != nil {
		return nil, err
	}

	type info struct {
		SPID    int64                   `db:"sp_id"`
		Sector  abi.SectorNumber        `db:"sector_num"`
		Offset  int64                   `db:"piece_offset"`
		Length  int64                   `db:"piece_length"`
		RawSize int64                   `db:"raw_size"`
		Proof   abi.RegisteredSealProof `db:"reg_seal_proof"`
	}

	pieceMap := make(map[abi.PieceInfo][]info)

	for _, pi := range pis {
		if _, ok := pieceMap[pi]; ok {
			continue
		}

		var infos []info

		err := p.db.Select(ctx, &infos, `SELECT
											mpd.sp_id,
											mpd.sector_num,
											mpd.piece_offset,
											mpd.piece_length,
											mpd.raw_size,
											sm.reg_seal_proof
										FROM
											market_piece_deal mpd
										INNER JOIN
											sectors_meta sm
										ON
											mpd.sp_id = sm.sp_id
											AND mpd.sector_num = sm.sector_num
										WHERE piece_cid = $1`, pi.PieceCID.String())

		if err != nil {
			return nil, xerrors.Errorf("getting deal info from database: %w", err)
		}
		pieceMap[pi] = infos
	}

	// Chunk the piece and generate links
	for pi, infos := range pieceMap {
		for _, i := range infos {
			unsealed, err := p.pieceProvider.IsUnsealed(ctx, storiface.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(i.SPID),
					Number: i.Sector,
				},
				ProofType: i.Proof,
			}, storiface.UnpaddedByteIndex(i.Offset), pi.Size.Unpadded())
			if err != nil {
				return nil, xerrors.Errorf("checking if sector is unsealed :%w", err)
			}

			if !unsealed {
				continue
			}

			reader, err := p.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(i.SPID),
					Number: i.Sector,
				},
				ProofType: i.Proof,
			}, storiface.UnpaddedByteIndex(i.Offset), abi.UnpaddedPieceSize(pi.Size), pi.PieceCID)
			if err != nil {
				return nil, xerrors.Errorf("getting piece reader: %w", err)
			}

			var recs []index.Record
			opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
			blockReader, err := carv2.NewBlockReader(reader, opts...)
			if err != nil {
				return nil, fmt.Errorf("getting block reader over piece: %w", err)
			}

			blockMetadata, err := blockReader.SkipNext()
			for err == nil {
				recs = append(recs, index.Record{
					Cid:    blockMetadata.Cid,
					Offset: blockMetadata.Offset,
				})
				blockMetadata, err = blockReader.SkipNext()
			}
			if !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("generating index for piece: %w", err)
			}

			mis := make(index.MultihashIndexSorted)
			err = mis.Load(recs)
			if err != nil {
				return nil, xerrors.Errorf("failed to load indexed in multihash sorter: %w", err)
			}

			// To avoid - Cannot assert pinter to interface
			idxF := func(sorted *index.MultihashIndexSorted) index.Index {
				return sorted
			}

			idx := idxF(&mis)
			iterableIndex := idx.(index.IterableIndex)

			mhi, err := chunker.CarMultihashIterator(iterableIndex)
			if err != nil {
				return nil, xerrors.Errorf("getting CAR multihash iterator: %w", err)
			}

			_, err = p.entriesChunker.Chunk(*mhi)
			if err != nil {
				return nil, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
			}

			if value, ok := p.cache.Get(cacheKey); ok {
				b := new(bytes.Buffer)
				err := dagjson.Encode(value, b)
				if err != nil {
					return nil, err
				}
				return b.Bytes(), nil
			}
		}
	}

	return nil, ErrNotFound
}

func (p *Provider) handleGet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pp := strings.TrimPrefix(r.URL.RawPath, p.httpPrefix+ProviderPath)
	pps := strings.Split(pp, "/")
	providerID := pps[0]

	req := strings.TrimPrefix(pp, IPNIPath)
	switch req {
	case "head":
		sh, err := p.getHead(r.Context(), providerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "", http.StatusNoContent)
				return
			}
			log.Errorf("failed to get signed head for peer %s: %w", providerID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(sh)
		if err != nil {
			log.Errorw("failed to write HTTP response", "err", err)
		}
	default:
		b, err := cid.Parse(req)
		if err != nil {
			log.Debugw("invalid CID as path parameter while getting content", "request", req, "err", err)
			http.Error(w, "invalid CID: "+req, http.StatusBadRequest)
			return
		}
		ad, err := p.getAd(r.Context(), b, providerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Check if this is an entry CID
				entry, err := p.GetEntry(b, providerID)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						log.Debugw("No Content Found", "CID", b.String())
						http.Error(w, "", http.StatusNoContent)
						return
					}
					log.Errorf("failed to get entry %s for peer %s: %w", b.String(), providerID, err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, err = w.Write(entry)
				if err != nil {
					log.Errorw("failed to write HTTP response", "err", err)
				}
				return
			}
			log.Errorf("failed to get ad %s for peer %s: %w", b.String(), providerID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		adn, err := ad.ToNode()
		if err != nil {
			log.Errorf("failed to convert ad %s for peer %s to IPLD node: %w", b.String(), providerID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Use local buffer for better error handing
		resp := new(bytes.Buffer)
		err = dagjson.Encode(adn, resp)
		if err != nil {
			log.Errorf("failed to encode ad %s for peer %s: %w", b.String(), providerID, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(resp.Bytes())
		if err != nil {
			log.Errorw("failed to write HTTP response", "err", err)
		}
		return
	}
}

func contentRouter(r *mux.Router, p *Provider) {
	r.Methods("GET").Path("/*").HandlerFunc(p.handleGet)
}

func Routes(r *mux.Router, p *Provider) {
	contentRouter(r.PathPrefix(IPNIRoutePath).Subrouter(), p)
}

func (p *Provider) StartPublishing(ctx context.Context) {
	// A poller which publishes head for each provider
	// every 10 minutes
	ticker := time.NewTicker(10 * time.Minute)
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

func (p *Provider) getHTTPAddressForPeer(peer string) ([]multiaddr.Multiaddr, error) {
	var ret []multiaddr.Multiaddr
	for _, addr := range p.httpServerAddresses {
		a, err := multiaddr.NewMultiaddr(addr.String() + "/" + peer)
		if err != nil {
			return nil, err
		}
		ret = append(ret, a)
	}

	return ret, nil
}
