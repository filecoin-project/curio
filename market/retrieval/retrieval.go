package retrieval

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-graphsync/storeutil"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/frisbii"
	ipld "github.com/ipld/go-ipld-prime"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"
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

func NewRetrievalProvider(ctx context.Context, db *harmonydb.DB, idxStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader) *Provider {
	bs := remoteblockstore.NewRemoteBlockstore(idxStore, db, cpr)
	lsys := storeutil.LinkSystemForBlockstore(bs)
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
