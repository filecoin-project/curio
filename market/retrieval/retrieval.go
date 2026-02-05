package retrieval

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/arc/v2"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-graphsync/storeutil"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/frisbii"
	ipld "github.com/ipld/go-ipld-prime"
	"github.com/snadrus/must"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"

	"github.com/filecoin-project/lotus/blockstore"
)

var log = logging.Logger("retrievals")

// activeRequestCounters stores atomic counters for active requests per path+method
var activeRequestCounters sync.Map // map[string]*atomic.Int64

// Request limiters using buffered channels as semaphores
const maxParallelRequests = 256

var (
	ipfsRequestLimiter     = make(chan struct{}, maxParallelRequests)
	ipfsHeadRequestLimiter = make(chan struct{}, maxParallelRequests/2)
	pieceRequestLimiter    = make(chan struct{}, maxParallelRequests)
)

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

// responseWriterWrapper wraps http.ResponseWriter to capture status code and bytes written
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
	isHead       bool
}

func (rw *responseWriterWrapper) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)

	if !rw.isHead {
		rw.bytesWritten += int64(n)
	}

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

// getActiveRequestCounter gets or creates an atomic counter for a specific path+method combination
func getActiveRequestCounter(pathPrefix, method string) *atomic.Int64 {
	key := pathPrefix + ":" + method

	// Try to load existing counter
	if counter, ok := activeRequestCounters.Load(key); ok {
		return counter.(*atomic.Int64)
	}

	// Create new counter
	counter := &atomic.Int64{}
	actual, loaded := activeRequestCounters.LoadOrStore(key, counter)
	if loaded {
		return actual.(*atomic.Int64)
	}
	return counter
}

// incrementActiveRequests atomically increments the active request counter and records the metric
func incrementActiveRequests(ctx context.Context, pathPrefix, method string) *atomic.Int64 {
	counter := getActiveRequestCounter(pathPrefix, method)
	newValue := counter.Add(1)

	// Record the new active request count
	_ = stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix),
		tag.Upsert(remoteblockstore.HttpMethodKey, method),
	}, remoteblockstore.HttpActiveRequests.M(newValue))

	return counter
}

// decrementActiveRequests atomically decrements the active request counter and records the metric
func decrementActiveRequests(ctx context.Context, counter *atomic.Int64, pathPrefix, method string) {
	newValue := counter.Add(-1)

	// Record the new active request count
	_ = stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix),
		tag.Upsert(remoteblockstore.HttpMethodKey, method),
	}, remoteblockstore.HttpActiveRequests.M(newValue))
}

// limiterMiddleware limits concurrent requests to the retrieval service
func limiterMiddleware(limiter chan struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try to acquire semaphore (non-blocking)
			select {
			case limiter <- struct{}{}:
				// Successfully acquired, ensure we release when done
				defer func() { <-limiter }()
				// Continue to next handler
				next.ServeHTTP(w, r)
			default:
				// Limit reached, return 429
				log.Warnw("Request limit reached", "method", r.Method, "path", r.URL.Path)
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("Service temporarily unavailable: too many concurrent requests"))
			}
		})
	}
}

// metricsMiddleware records HTTP metrics for requests
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract path prefix to avoid unbounded cardinality in Prometheus
		pathPrefix := getPathPrefix(r.URL.Path)

		// Record request count and increment active requests
		ctx, _ := tag.New(r.Context(), tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix), tag.Upsert(remoteblockstore.HttpMethodKey, r.Method))
		stats.Record(ctx, remoteblockstore.HttpRequestCount.M(1))

		// Increment active requests counter
		counter := incrementActiveRequests(ctx, pathPrefix, r.Method)

		// Ensure we decrement when done
		defer decrementActiveRequests(ctx, counter, pathPrefix, r.Method)

		// Wrap response writer to capture status and bytes
		wrapper := &responseWriterWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default if WriteHeader is not called
			isHead:         r.Method == http.MethodHead,
		}

		// Serve the request
		next.ServeHTTP(wrapper, r.WithContext(ctx))

		// Record response metrics
		statusCodeStr := strconv.Itoa(wrapper.statusCode)
		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(remoteblockstore.HttpStatusCodeKey, statusCodeStr),
			tag.Upsert(remoteblockstore.HttpPathKey, pathPrefix),
			tag.Upsert(remoteblockstore.HttpMethodKey, r.Method),
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

		// Piece endpoint with limiter
		r.Group(func(r chi.Router) {
			r.Use(limiterMiddleware(pieceRequestLimiter))
			r.Get(piecePrefix+"{cid}", rp.handleByPieceCid)
		})

		// IPFS endpoints with limiter
		r.Group(func(r chi.Router) {
			r.Use(limiterMiddleware(ipfsHeadRequestLimiter))
			r.Head(ipfsPrefix+"*", rp.fr.ServeHTTP)
		})

		r.Group(func(r chi.Router) {
			r.Use(limiterMiddleware(ipfsRequestLimiter))
			r.Get(ipfsPrefix+"*", rp.fr.ServeHTTP)
		})

		// Info endpoint without limiter
		r.Get(infoPage, handleInfo)
	})
}

func handleInfo(rw http.ResponseWriter, r *http.Request) {
	infoOut := fmt.Sprintf(`{"Version":"0.4.0", "Server": "Curio/%s"}`, build.BuildVersion)

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
