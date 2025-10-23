package retrieval

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/arc/v2"
	"github.com/ipfs/go-graphsync/storeutil"
	blocks "github.com/ipfs/go-block-format"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/frisbii"
	ipld "github.com/ipld/go-ipld-prime"
	"github.com/snadrus/must"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"
	"github.com/filecoin-project/lotus/blockstore"
)

var log = logging.Logger("retrievals")

type Provider struct {
	db  *harmonydb.DB
	bs  *remoteblockstore.RemoteBlockstore
	fr  *frisbii.HttpIpfs
	cpr *cachedreader.CachedPieceReader
}

const (
	piecePrefix = "/piece/"
	ipfsPrefix  = "/ipfs/"
	infoPage    = "/info"
)

var RetrievalBlockCache = must.One(lru.NewARC[blockstore.MhString, blocks.Block](4096))

func NewRetrievalProvider(ctx context.Context, db *harmonydb.DB, idxStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader) *Provider {
	bs := remoteblockstore.NewRemoteBlockstore(idxStore, db, cpr)
	cbs := blockstore.NewReadCachedBlockstore(blockstore.Adapt(bs), &BlockstoreCacheWrap[blockstore.MhString]{Sub: RetrievalBlockCache})

	lsys := storeutil.LinkSystemForBlockstore(cbs)
	fr := frisbii.NewHttpIpfs(ctx, lsys)

	return &Provider{
		db:  db,
		bs:  bs,
		fr:  fr,
		cpr: cpr,
	}
}

// NewRetrievalProviderWithLinkSystem creates a Provider with a custom LinkSystem for testing
func NewRetrievalProviderWithLinkSystem(ctx context.Context, lsys ipld.LinkSystem) *Provider {
	fr := frisbii.NewHttpIpfs(ctx, lsys)

	return &Provider{
		fr: fr,
	}
}

// logRequest logs incoming HTTP requests with their headers
func logRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Infow("HTTP request",
			"method", r.Method,
			"url", r.URL.String(),
			"remote_addr", r.RemoteAddr,
			"headers", r.Header)
		next(w, r)
	}
}

func Router(mux *chi.Mux, rp *Provider) {
	mux.Get(piecePrefix+"{cid}", logRequest(rp.handleByPieceCid))
	mux.Get(ipfsPrefix+"*", logRequest(rp.fr.ServeHTTP))
	mux.Head(ipfsPrefix+"*", logRequest(rp.fr.ServeHTTP))
	mux.Get(infoPage, logRequest(handleInfo))
}

func handleInfo(rw http.ResponseWriter, r *http.Request) {
	const infoOut = `{"Version":"0.4.0", "Server": "Curio/0.0.0"}`

	_, _ = rw.Write([]byte(infoOut))
}


type BlockstoreCacheWrap[T any] struct {
	Sub interface {
		Remove(mhString T)
		Contains(mhString T) bool
		Get(mhString T) (blocks.Block, bool)
		Add(mhString T, block blocks.Block)
	}
}

func (b *BlockstoreCacheWrap[T]) Contains(mhString T) bool {
	return b.Sub.Contains(mhString)
}

func (b *BlockstoreCacheWrap[T]) Get(mhString T) (blocks.Block, bool) {
	return b.Sub.Get(mhString)
}

func (b *BlockstoreCacheWrap[T]) Remove(mhString T) bool {
	b.Sub.Remove(mhString)
	return true
}

func (b *BlockstoreCacheWrap[T]) Add(mhString T, block blocks.Block) (evicted bool) {
	b.Sub.Add(mhString, block)
	return true
}

var _ blockstore.BlockstoreCache = (*BlockstoreCacheWrap[blockstore.MhString])(nil)
