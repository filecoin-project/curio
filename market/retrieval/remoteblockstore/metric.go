package remoteblockstore

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	lotusmetrics "github.com/filecoin-project/lotus/metrics"
)

// Tag keys for PDP metrics
var (
	domainTagKey, _    = tag.NewKey("domain")
	pieceSizeTagKey, _ = tag.NewKey("piece_size_category")
	pieceCIDTagKey, _  = tag.NewKey("piece_cid")
	statusTagKey, _    = tag.NewKey("status")
)

// Distribution
var defaultMillisecondsDistribution = view.Distribution(
	1,      // 1 millisecond
	5,      // 5 milliseconds
	10,     // 10 milliseconds
	20,     // 20 milliseconds
	50,     // 50 milliseconds
	100,    // 100 milliseconds
	200,    // 200 milliseconds
	500,    // 500 milliseconds
	1000,   // 1 second
	2000,   // 2 seconds
	5000,   // 5 seconds
	10000,  // 10 seconds
	20000,  // 20 seconds
	50000,  // 50 seconds
	100000, // 100 seconds
)

// Size buckets for piece sizes (in MB)
var sizeBuckets = []float64{0.1, 1, 10, 50, 100, 500, 1000, 5000, 10000}

var (
	RetrievalInfo = stats.Int64("retrieval_info", "Arbitrary counter to tag node info to", stats.UnitDimensionless)
	// piece
	HttpPieceByCidRequestCount     = stats.Int64("http/piece_by_cid_request_count", "Counter of /piece/<piece-cid> requests", stats.UnitDimensionless)
	HttpPieceByCidRequestDuration  = stats.Float64("http/piece_by_cid_request_duration_ms", "Time spent retrieving a piece by cid", stats.UnitMilliseconds)
	HttpPieceByCid200ResponseCount = stats.Int64("http/piece_by_cid_200_response_count", "Counter of /piece/<piece-cid> 200 responses", stats.UnitDimensionless)
	HttpPieceByCid400ResponseCount = stats.Int64("http/piece_by_cid_400_response_count", "Counter of /piece/<piece-cid> 400 responses", stats.UnitDimensionless)
	HttpPieceByCid404ResponseCount = stats.Int64("http/piece_by_cid_404_response_count", "Counter of /piece/<piece-cid> 404 responses", stats.UnitDimensionless)
	HttpPieceByCid500ResponseCount = stats.Int64("http/piece_by_cid_500_response_count", "Counter of /piece/<piece-cid> 500 responses", stats.UnitDimensionless)
	// PDP metrics
	HttpPdpPieceAccessCount       = stats.Int64("http/pdp_piece_access_count", "Number of times PDP pieces are accessed via main retrieval endpoint", stats.UnitDimensionless)
	HttpPdpPieceAccessDomainCount = stats.Int64("http/pdp_piece_access_domain_count", "Number of PDP piece accesses by domain", stats.UnitDimensionless)
	HttpPdpPieceBytesServedCount  = stats.Int64("http/pdp_piece_bytes_served_count", "Total bytes served for PDP pieces", stats.UnitBytes)
	HttpPdpPieceSizeDistribution  = stats.Int64("http/pdp_piece_size_distribution", "Size distribution of PDP pieces accessed", stats.UnitBytes)
	HttpPdpPieceTTFB              = stats.Float64("http/pdp_piece_ttfb_ms", "Time to first byte for PDP piece retrieval", stats.UnitMilliseconds)
	HttpPdpPieceRetrievalCount    = stats.Int64("http/pdp_piece_retrieval_count", "Number of PDP piece retrieval attempts by status", stats.UnitDimensionless)
	// Gateway
	HttpRblsGetRequestCount             = stats.Int64("http/rbls_get_request_count", "Counter of RemoteBlockstore Get requests", stats.UnitDimensionless)
	HttpRblsGetSuccessResponseCount     = stats.Int64("http/rbls_get_success_response_count", "Counter of successful RemoteBlockstore Get responses", stats.UnitDimensionless)
	HttpRblsGetFailResponseCount        = stats.Int64("http/rbls_get_fail_response_count", "Counter of failed RemoteBlockstore Get responses", stats.UnitDimensionless)
	HttpRblsGetSizeRequestCount         = stats.Int64("http/rbls_getsize_request_count", "Counter of RemoteBlockstore GetSize requests", stats.UnitDimensionless)
	HttpRblsGetSizeSuccessResponseCount = stats.Int64("http/rbls_getsize_success_response_count", "Counter of successful RemoteBlockstore GetSize responses", stats.UnitDimensionless)
	HttpRblsGetSizeFailResponseCount    = stats.Int64("http/rbls_getsize_fail_response_count", "Counter of failed RemoteBlockstore GetSize responses", stats.UnitDimensionless)
	HttpRblsHasRequestCount             = stats.Int64("http/rbls_has_request_count", "Counter of RemoteBlockstore Has requests", stats.UnitDimensionless)
	HttpRblsHasSuccessResponseCount     = stats.Int64("http/rbls_has_success_response_count", "Counter of successful RemoteBlockstore Has responses", stats.UnitDimensionless)
	HttpRblsHasFailResponseCount        = stats.Int64("http/rbls_has_fail_response_count", "Counter of failed RemoteBlockstore Has responses", stats.UnitDimensionless)
	HttpRblsBytesSentCount              = stats.Int64("http/rbls_bytes_sent_count", "Counter of the number of bytes sent by bitswap since startup", stats.UnitBytes)
)

var (
	HttpPieceByCidRequestCountView = &view.View{
		Measure:     HttpPieceByCidRequestCount,
		Aggregation: view.Count(),
	}
	HttpPieceByCidRequestDurationView = &view.View{
		Measure:     HttpPieceByCidRequestDuration,
		Aggregation: defaultMillisecondsDistribution,
	}
	HttpPieceByCid200ResponseCountView = &view.View{
		Measure:     HttpPieceByCid200ResponseCount,
		Aggregation: view.Count(),
	}
	HttpPieceByCid400ResponseCountView = &view.View{
		Measure:     HttpPieceByCid400ResponseCount,
		Aggregation: view.Count(),
	}
	HttpPieceByCid404ResponseCountView = &view.View{
		Measure:     HttpPieceByCid404ResponseCount,
		Aggregation: view.Count(),
	}
	HttpPieceByCid500ResponseCountView = &view.View{
		Measure:     HttpPieceByCid500ResponseCount,
		Aggregation: view.Count(),
	}

	HttpRblsGetRequestCountView = &view.View{
		Measure:     HttpRblsGetRequestCount,
		Aggregation: view.Count(),
	}
	HttpRblsGetSuccessResponseCountView = &view.View{
		Measure:     HttpRblsGetSuccessResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsGetFailResponseCountView = &view.View{
		Measure:     HttpRblsGetFailResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsGetSizeRequestCountView = &view.View{
		Measure:     HttpRblsGetSizeRequestCount,
		Aggregation: view.Count(),
	}
	HttpRblsGetSizeSuccessResponseCountView = &view.View{
		Measure:     HttpRblsGetSizeSuccessResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsGetSizeFailResponseCountView = &view.View{
		Measure:     HttpRblsGetSizeFailResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsHasRequestCountView = &view.View{
		Measure:     HttpRblsHasRequestCount,
		Aggregation: view.Count(),
	}
	HttpRblsHasSuccessResponseCountView = &view.View{
		Measure:     HttpRblsHasSuccessResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsHasFailResponseCountView = &view.View{
		Measure:     HttpRblsHasFailResponseCount,
		Aggregation: view.Count(),
	}
	HttpRblsBytesSentCountView = &view.View{
		Measure:     HttpRblsBytesSentCount,
		Aggregation: view.Sum(),
	}
	HttpPdpPieceAccessCountView = &view.View{
		Measure:     HttpPdpPieceAccessCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{},
	}
	HttpPdpPieceAccessDomainCountView = &view.View{
		Measure:     HttpPdpPieceAccessDomainCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{domainTagKey, pieceCIDTagKey},
	}
	HttpPdpPieceBytesServedCountView = &view.View{
		Measure:     HttpPdpPieceBytesServedCount,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{domainTagKey, pieceCIDTagKey},
	}
	HttpPdpPieceSizeDistributionView = &view.View{
		Measure:     HttpPdpPieceSizeDistribution,
		Aggregation: view.Distribution(sizeBuckets...),
		TagKeys:     []tag.Key{pieceSizeTagKey, pieceCIDTagKey},
	}
	HttpPdpPieceTTFBView = &view.View{
		Measure:     HttpPdpPieceTTFB,
		Aggregation: defaultMillisecondsDistribution,
		TagKeys:     []tag.Key{domainTagKey, pieceSizeTagKey, pieceCIDTagKey},
	}
	HttpPdpPieceRetrievalCountView = &view.View{
		Measure:     HttpPdpPieceRetrievalCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{statusTagKey, domainTagKey, pieceCIDTagKey},
	}
)

// CacheViews groups all cache-related default views.
func init() {
	err := view.Register(
		HttpPieceByCidRequestCountView,
		HttpPieceByCidRequestDurationView,
		HttpPieceByCid200ResponseCountView,
		HttpPieceByCid400ResponseCountView,
		HttpPieceByCid404ResponseCountView,
		HttpPieceByCid500ResponseCountView,
		HttpRblsGetRequestCountView,
		HttpRblsGetSuccessResponseCountView,
		HttpRblsGetFailResponseCountView,
		HttpRblsGetSizeRequestCountView,
		HttpRblsGetSizeSuccessResponseCountView,
		HttpRblsGetSizeFailResponseCountView,
		HttpRblsHasRequestCountView,
		HttpRblsHasSuccessResponseCountView,
		HttpRblsHasFailResponseCountView,
		HttpRblsBytesSentCountView,
		HttpPdpPieceAccessCountView,
		HttpPdpPieceAccessDomainCountView,
		HttpPdpPieceBytesServedCountView,
		HttpPdpPieceSizeDistributionView,
		HttpPdpPieceTTFBView,
		HttpPdpPieceRetrievalCountView,

		lotusmetrics.DagStorePRBytesDiscardedView,
		lotusmetrics.DagStorePRBytesRequestedView,
		lotusmetrics.DagStorePRDiscardCountView,
		lotusmetrics.DagStorePRInitCountView,
		lotusmetrics.DagStorePRSeekBackBytesView,
		lotusmetrics.DagStorePRSeekBackCountView,
		lotusmetrics.DagStorePRSeekForwardBytesView,
		lotusmetrics.DagStorePRSeekForwardCountView,

		lotusmetrics.DagStorePRAtHitBytesView,
		lotusmetrics.DagStorePRAtHitCountView,
		lotusmetrics.DagStorePRAtCacheFillCountView,
		lotusmetrics.DagStorePRAtReadBytesView,
		lotusmetrics.DagStorePRAtReadCountView,
	)
	if err != nil {
		panic(err)
	}
}

// categorizeSize returns a category string for the given size in bytes
func categorizeSize(sizeBytes int64) string {
	sizeMB := float64(sizeBytes) / (1024 * 1024)

	switch {
	case sizeMB < 1:
		return "small"
	case sizeMB < 100:
		return "medium"
	case sizeMB < 1000:
		return "large"
	default:
		return "xlarge"
	}
}

// extractDomain extracts the domain from the request for metrics
func extractDomain(r *http.Request) string {
	// Try to get domain from Referer header first
	if referer := r.Header.Get("Referer"); referer != "" {
		if u, err := url.Parse(referer); err == nil && u.Host != "" {
			return u.Host
		}
	}

	// Try to get domain from Host header
	if host := r.Header.Get("Host"); host != "" {
		return host
	}

	// Try to get from X-Forwarded-Host header
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		return forwardedHost
	}

	// Fall back to remote address
	if remoteAddr := r.RemoteAddr; remoteAddr != "" {
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			return host
		}
		return remoteAddr
	}

	return "unknown"
}

// RecordPDPPieceRetrieval records metrics for PDP piece retrieval attempts (both success and failure)
func RecordPDPPieceRetrieval(ctx context.Context, r *http.Request, pieceCID string, status string) {
	domain := extractDomain(r)

	// Record retrieval attempt with status
	if retrievalCtx, err := tag.New(ctx, tag.Insert(statusTagKey, status), tag.Insert(domainTagKey, domain), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(retrievalCtx, HttpPdpPieceRetrievalCount.M(1))
	}
}

// RecordPDPPieceAccess records metrics when a PDP piece is successfully accessed via the main retrieval endpoint
func RecordPDPPieceAccess(ctx context.Context, r *http.Request, pieceCID string, pieceSize int64, ttfbMs float64) {
	domain := extractDomain(r)
	sizeCategory := categorizeSize(pieceSize)

	// Record basic access
	stats.Record(ctx, HttpPdpPieceAccessCount.M(1))

	// Record access by domain and piece CID
	if domainCtx, err := tag.New(ctx, tag.Insert(domainTagKey, domain), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(domainCtx, HttpPdpPieceAccessDomainCount.M(1))
		stats.Record(domainCtx, HttpPdpPieceBytesServedCount.M(pieceSize))
	}

	// Record piece size distribution
	if sizeCtx, err := tag.New(ctx, tag.Insert(pieceSizeTagKey, sizeCategory), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(sizeCtx, HttpPdpPieceSizeDistribution.M(pieceSize))
	}

	// Record TTFB with domain, size category, and piece CID tags
	if ttfbCtx, err := tag.New(ctx, tag.Insert(domainTagKey, domain), tag.Insert(pieceSizeTagKey, sizeCategory), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(ttfbCtx, HttpPdpPieceTTFB.M(ttfbMs))
	}
}
