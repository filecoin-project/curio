package sealmarket

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// Tag keys for remote seal metrics.
var (
	RsealEndpointKey, _   = tag.NewKey("endpoint")
	RsealMethodKey, _     = tag.NewKey("method")
	RsealStatusCodeKey, _ = tag.NewKey("status_code")
	RsealAcceptedKey, _   = tag.NewKey("accepted")
)

// Duration distribution tuned for remote seal workloads.
// Range: 100ms to 30 minutes. Sealed-data transfers (32 GiB) and commit1
// FFI computation can take minutes; lighter endpoints finish in <1s.
var rsealDurationDistribution = view.Distribution(
	100,     // 100ms
	500,     // 500ms
	1000,    // 1s
	2000,    // 2s
	5000,    // 5s
	10000,   // 10s
	30000,   // 30s
	60000,   // 1min
	120000,  // 2min
	300000,  // 5min
	600000,  // 10min
	1800000, // 30min
)

// Commit1 FFI compute distribution, tuned for CPU/GPU-bound work.
// Range: 1s to 10 minutes.
var rsealComputeDistribution = view.Distribution(
	1000,   // 1s
	5000,   // 5s
	10000,  // 10s
	30000,  // 30s
	60000,  // 1min
	120000, // 2min
	300000, // 5min
	600000, // 10min
)

// Tier 1: Cross-cutting middleware measures.
var (
	// RsealRequestCount counts HTTP requests to remote seal endpoints.
	RsealRequestCount = stats.Int64("rseal/request_count", "Counter of remote seal HTTP requests", stats.UnitDimensionless)

	// RsealResponseStatusCount counts HTTP response status codes.
	RsealResponseStatusCount = stats.Int64("rseal/response_status_count", "Counter of remote seal HTTP response status codes", stats.UnitDimensionless)

	// RsealResponseBytesCount sums HTTP response bytes written.
	RsealResponseBytesCount = stats.Int64("rseal/response_bytes_count", "Sum of remote seal HTTP response bytes", stats.UnitBytes)

	// RsealActiveRequests tracks in-flight requests per endpoint.
	RsealActiveRequests = stats.Int64("rseal/active_requests", "Number of active remote seal HTTP requests", stats.UnitDimensionless)

	// RsealRequestDuration records request latency in milliseconds.
	RsealRequestDuration = stats.Float64("rseal/request_duration_ms", "Remote seal HTTP request duration", stats.UnitMilliseconds)
)

// Tier 3: Business-level measures.
var (
	// RsealOrdersTotal counts order attempts, labeled by accepted (true/false).
	RsealOrdersTotal = stats.Int64("rseal/orders_total", "Counter of remote seal order attempts", stats.UnitDimensionless)

	// RsealSlotsIssued counts slot tokens issued by /available.
	RsealSlotsIssued = stats.Int64("rseal/slots_issued_total", "Counter of remote seal slot tokens issued", stats.UnitDimensionless)

	// RsealCommit1ComputeDuration records the C1 FFI computation time in milliseconds
	// (excluding network I/O, DB queries, etc.).
	RsealCommit1ComputeDuration = stats.Float64("rseal/commit1_compute_duration_ms", "Remote seal commit1 FFI compute duration", stats.UnitMilliseconds)
)

// Views define how the measures are aggregated and exported to Prometheus.
var (
	RsealRequestCountView = &view.View{
		Measure:     RsealRequestCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RsealEndpointKey, RsealMethodKey},
	}
	RsealResponseStatusCountView = &view.View{
		Measure:     RsealResponseStatusCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RsealEndpointKey, RsealMethodKey, RsealStatusCodeKey},
	}
	RsealResponseBytesCountView = &view.View{
		Measure:     RsealResponseBytesCount,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{RsealEndpointKey, RsealStatusCodeKey},
	}
	RsealActiveRequestsView = &view.View{
		Measure:     RsealActiveRequests,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{RsealEndpointKey, RsealMethodKey},
	}
	RsealRequestDurationView = &view.View{
		Measure:     RsealRequestDuration,
		Aggregation: rsealDurationDistribution,
		TagKeys:     []tag.Key{RsealEndpointKey, RsealMethodKey},
	}

	RsealOrdersTotalView = &view.View{
		Measure:     RsealOrdersTotal,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RsealAcceptedKey},
	}
	RsealSlotsIssuedView = &view.View{
		Measure:     RsealSlotsIssued,
		Aggregation: view.Count(),
	}
	RsealCommit1ComputeDurationView = &view.View{
		Measure:     RsealCommit1ComputeDuration,
		Aggregation: rsealComputeDistribution,
	}
)

func init() {
	err := view.Register(
		RsealRequestCountView,
		RsealResponseStatusCountView,
		RsealResponseBytesCountView,
		RsealActiveRequestsView,
		RsealRequestDurationView,
		RsealOrdersTotalView,
		RsealSlotsIssuedView,
		RsealCommit1ComputeDurationView,
	)
	if err != nil {
		panic(err)
	}
}

// --- Metrics middleware ---

// rsealActiveCounters maps "endpoint:method" → *atomic.Int64 for the gauge.
var rsealActiveCounters sync.Map

func rsealIncrementActive(endpoint, method string) *atomic.Int64 {
	key := endpoint + ":" + method
	if v, ok := rsealActiveCounters.Load(key); ok {
		counter := v.(*atomic.Int64)
		counter.Add(1)
		return counter
	}
	counter := &atomic.Int64{}
	actual, _ := rsealActiveCounters.LoadOrStore(key, counter)
	c := actual.(*atomic.Int64)
	c.Add(1)
	return c
}

func rsealDecrementActive(ctx context.Context, counter *atomic.Int64, endpoint, method string) {
	val := counter.Add(-1)
	_ = stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(RsealEndpointKey, endpoint),
		tag.Upsert(RsealMethodKey, method),
	}, RsealActiveRequests.M(val))
}

// rsealResponseWriter wraps http.ResponseWriter to capture status code and bytes written.
type rsealResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *rsealResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *rsealResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Unwrap exposes the underlying ResponseWriter for http.ServeFile to detect
// io.ReadFrom support (sendfile).
func (rw *rsealResponseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// rsealEndpointName extracts a short, low-cardinality endpoint label from a request path.
// The path is expected to start with DelegatedSealPath ("/remoteseal/delegated/v0/").
// Examples:
//
//	"/remoteseal/delegated/v0/capabilities"             → "capabilities"
//	"/remoteseal/delegated/v0/sealed-data/1234/5"       → "sealed-data"
//	"/remoteseal/delegated/v0/cache-data/1234/5"        → "cache-data"
//	"/remoteseal/delegated/v0/commit1"                  → "commit1"
func rsealEndpointName(urlPath string) string {
	// Strip the base prefix
	rest := strings.TrimPrefix(urlPath, DelegatedSealPath)
	if rest == urlPath {
		// Path didn't have the expected prefix — fallback
		return "unknown"
	}

	// Take the first segment (before any '/')
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		rest = rest[:idx]
	}

	if rest == "" {
		return "unknown"
	}

	return rest
}

// rsealMetricsMiddleware is a chi middleware that records OpenCensus metrics
// for all remote seal endpoints. It mirrors the retrieval metricsMiddleware
// pattern: request count, response status/bytes, in-flight gauge, and latency.
func rsealMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := rsealEndpointName(r.URL.Path)
		start := time.Now()

		// Record request count and set up context with tags
		ctx, _ := tag.New(r.Context(),
			tag.Upsert(RsealEndpointKey, endpoint),
			tag.Upsert(RsealMethodKey, r.Method),
		)
		stats.Record(ctx, RsealRequestCount.M(1))

		// Track in-flight requests
		counter := rsealIncrementActive(endpoint, r.Method)
		defer rsealDecrementActive(ctx, counter, endpoint, r.Method)

		// Wrap response writer to capture status and bytes
		wrapper := &rsealResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default if WriteHeader is not called
		}

		// Serve the request
		next.ServeHTTP(wrapper, r.WithContext(ctx))

		// Record response metrics
		elapsed := float64(time.Since(start).Milliseconds())
		statusStr := strconv.Itoa(wrapper.statusCode)

		_ = stats.RecordWithTags(ctx, []tag.Mutator{
			tag.Upsert(RsealStatusCodeKey, statusStr),
		},
			RsealResponseStatusCount.M(1),
			RsealResponseBytesCount.M(wrapper.bytesWritten),
		)
		stats.Record(ctx, RsealRequestDuration.M(elapsed))
	})
}
