package ipni_provider

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	"github.com/ipni/go-libipni/dagsync/ipnisync/head"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/urltomultiaddr"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"

	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const IPNIRoutePath = "/ipni-provider/"
const IPNIPath = "/ipni/v1/ad/"
const publishInterval = 10 * time.Minute

var validate = true

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
	api           ipniAPI
	db            *harmonydb.DB
	pieceProvider *pieceprovider.PieceProvider
	indexStore    *indexstore.IndexStore
	httpPrefix    string
	keys          map[string]*peerInfo // map[peerID String]Private_Key
	// announceURLs enables sending direct announcements via HTTP. This is
	// the list of indexer URLs to send direct HTTP announce messages to.
	announceURLs        []*url.URL
	httpServerAddresses []multiaddr.Multiaddr
}

func NewProvider(d *deps.Deps) (*Provider, error) {
	ctx := context.Background()

	keyMap := make(map[string]*peerInfo)

	rows, err := d.DB.Query(ctx, `SELECT priv_key FROM libp2p`)
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

	announceURLs := make([]*url.URL, 0, len(d.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs))

	for i, us := range d.Cfg.Market.StorageMarketConfig.IPNI.DirectAnnounceURLs {
		u, err := url.Parse(us)
		if err != nil {
			return nil, err
		}
		announceURLs[i] = u
	}

	httpServerAddresses := make([]multiaddr.Multiaddr, 0, len(d.Cfg.Market.StorageMarketConfig.IPNI.AnnounceAddresses))

	for i, a := range d.Cfg.Market.StorageMarketConfig.IPNI.AnnounceAddresses {
		addr, err := urltomultiaddr.UrlToMultiaddr(a)
		if err != nil {
			return nil, err
		}
		addr, err = multiaddr.NewMultiaddr(addr.String() + IPNIRoutePath)
		if err != nil {
			return nil, err
		}
		httpServerAddresses[i] = addr
	}

	return &Provider{
		api:                 d.Chain,
		db:                  d.DB,
		pieceProvider:       d.PieceProvider,
		indexStore:          d.IndexStore,
		keys:                keyMap,
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

	signedHead, err := head.NewSignedHead(lnk.(cidlink.Link).Cid, "", p.keys[provider].Key)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate signed head for peer %s: %w", provider, err)
	}

	return signedHead.Encode()
}

func (p *Provider) getEntry(block cid.Cid, provider string) ([]byte, error) {
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

		firstCid, err := cid.Parse(*chunk.FirstCID)
		if err != nil {
			return nil, xerrors.Errorf("parsing first CID: %w", err)
		}

		var next ipld.Link
		if chunk.PrevCID != nil {
			prevChunk, err := cid.Parse(*chunk.PrevCID)
			if err != nil {
				return nil, xerrors.Errorf("parsing previous CID: %w", err)
			}

			next = cidlink.Link{Cid: prevChunk}
		}

		return p.reconstructChunkFromDB(ctx, block, pieceCid, firstCid, next, chunk.NumBlocks)
	}

	return p.reconstructChunkFromCar(ctx, block, pieceCid, *chunk.StartOffset, nil, chunk.NumBlocks)
}

func (p *Provider) reconstructChunkFromCar(ctx context.Context, chunk, piece cid.Cid, startOff int64, next ipld.Link, numBlocks int64) ([]byte, error) {
	type info struct {
		SPID    int64                   `db:"sp_id"`
		Sector  abi.SectorNumber        `db:"sector_num"`
		Offset  int64                   `db:"piece_offset"`
		Length  int64                   `db:"piece_length"`
		RawSize int64                   `db:"raw_size"`
		Proof   abi.RegisteredSealProof `db:"reg_seal_proof"`
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
		WHERE piece_cid = $1`, piece)
	if err != nil {
		return nil, xerrors.Errorf("getting deal info from database: %w", err)
	}

	for _, i := range infos {
		sref := storiface.SectorRef{
			ID: abi.SectorID{
				Miner:  abi.ActorID(i.SPID),
				Number: i.Sector,
			},
			ProofType: i.Proof,
		}

		reader, err := p.pieceProvider.ReadPiece(ctx, sref, storiface.UnpaddedByteIndex(i.Offset), abi.PaddedPieceSize(i.Length).Unpadded(), piece)
		if err != nil {
			log.Warnw("failed to read piece for ipni chunk reconstruction", "error", err, "sector", sref, "offset", i.Offset, "length", i.Length, "pieceCID", piece, "chunkCID", chunk)
			continue
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

	return nil, ErrNotFound
}

func (p *Provider) reconstructChunkFromDB(ctx context.Context, chunk, piece, firstCid cid.Cid, next ipld.Link, numBlocks int64) ([]byte, error) {
	mhs, err := p.indexStore.GetPieceHashRange(ctx, piece, firstCid.Hash(), numBlocks)
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

func (p *Provider) handleGet(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pp := strings.TrimPrefix(r.URL.RawPath, p.httpPrefix+IPNIRoutePath)
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
			http.Error(w, "", http.StatusInternalServerError)
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
				entry, err := p.getEntry(b, providerID)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						log.Debugw("No Content Found", "CID", b.String())
						http.Error(w, "", http.StatusNoContent)
						return
					}
					log.Errorf("failed to get entry %s for peer %s: %w", b.String(), providerID, err)
					http.Error(w, "", http.StatusInternalServerError)
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
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		adn, err := ad.ToNode()
		if err != nil {
			log.Errorf("failed to convert ad %s for peer %s to IPLD node: %w", b.String(), providerID, err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		// Use local buffer for better error handing
		resp := new(bytes.Buffer)
		err = dagjson.Encode(adn, resp)
		if err != nil {
			log.Errorf("failed to encode ad %s for peer %s: %w", b.String(), providerID, err)
			http.Error(w, "", http.StatusInternalServerError)
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

func Routes(r *chi.Mux, p *Provider) {
	r.Get(IPNIRoutePath, p.handleGet)
}

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
