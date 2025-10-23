package retrieval

import (
	"context"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-graphsync/storeutil"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/frisbii"
	ipld "github.com/ipld/go-ipld-prime"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

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

// responseWriterWrapper wraps http.ResponseWriter to capture status code and bytes written
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *responseWriterWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// getPathPrefix extracts the first path element to avoid cardinality explosion in metrics
// Examples: "/piece/abc123" -> "/piece/", "/ipfs/QmABC" -> "/ipfs/", "/info" -> "/info", "/" -> "/"
func getPathPrefix(urlPath string) string {
	// Clean the path to remove any .. or . elements
	cleaned := path.Clean(urlPath)

	if cleaned == "/" || cleaned == "" {
		return "/"
	}

	// Split the path and get the first element
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		return "/" + parts[0] + "/"
	}

	return "/"
}

// metricsMiddleware records HTTP metrics for requests
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract path prefix to avoid unbounded cardinality in Prometheus
		pathPrefix := getPathPrefix(r.URL.Path)

		// Record request count
		ctx, _ := tag.New(r.Context(), tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix))
		stats.Record(ctx, remoteblockstore.HttpRequestCount.M(1))

		// Wrap response writer to capture status and bytes
		wrapper := &responseWriterWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default if WriteHeader is not called
		}

		// Serve the request
		next.ServeHTTP(wrapper, r.WithContext(ctx))

		// Record response metrics
		statusCodeStr := strconv.Itoa(wrapper.statusCode)
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(remoteblockstore.HttpStatusCodeKey, statusCodeStr),
			tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix),
		},
			remoteblockstore.HttpResponseStatusCount.M(1),
			remoteblockstore.HttpResponseBytesCount.M(wrapper.bytesWritten),
		)

		log.Debugw("HTTP request", "method", r.Method, "path", r.URL.Path, "pathPrefix", pathPrefix, "status", wrapper.statusCode, "bytes", wrapper.bytesWritten)
	})
}

func Router(mux *chi.Mux, rp *Provider) {
	// Group retrieval routes with metrics middleware
	mux.Group(func(r chi.Router) {
		r.Use(metricsMiddleware)
		r.Get(piecePrefix+"{cid}", rp.handleByPieceCid)
		r.Get(ipfsPrefix+"*", rp.fr.ServeHTTP)
		r.Head(ipfsPrefix+"*", rp.fr.ServeHTTP)
		r.Get(infoPage, handleInfo)
	})
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
	block, ok := b.Sub.Get(mhString)
	if ok {
		stats.Record(context.Background(), remoteblockstore.BlockstoreCacheHits.M(1))
	} else {
		stats.Record(context.Background(), remoteblockstore.BlockstoreCacheMisses.M(1))
	}
	return block, ok
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
