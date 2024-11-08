package remoteblockstore

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"

	"github.com/filecoin-project/lotus/metrics"
	lotusmetrics "github.com/filecoin-project/lotus/metrics"
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

var (
	RetrievalInfo = stats.Int64("retrieval_info", "Arbitrary counter to tag node info to", stats.UnitDimensionless)
	// piece
	HttpPieceByCidRequestCount     = stats.Int64("http/piece_by_cid_request_count", "Counter of /piece/<piece-cid> requests", stats.UnitDimensionless)
	HttpPieceByCidRequestDuration  = stats.Float64("http/piece_by_cid_request_duration_ms", "Time spent retrieving a piece by cid", stats.UnitMilliseconds)
	HttpPieceByCid200ResponseCount = stats.Int64("http/piece_by_cid_200_response_count", "Counter of /piece/<piece-cid> 200 responses", stats.UnitDimensionless)
	HttpPieceByCid400ResponseCount = stats.Int64("http/piece_by_cid_400_response_count", "Counter of /piece/<piece-cid> 400 responses", stats.UnitDimensionless)
	HttpPieceByCid404ResponseCount = stats.Int64("http/piece_by_cid_404_response_count", "Counter of /piece/<piece-cid> 404 responses", stats.UnitDimensionless)
	HttpPieceByCid500ResponseCount = stats.Int64("http/piece_by_cid_500_response_count", "Counter of /piece/<piece-cid> 500 responses", stats.UnitDimensionless)
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
)

// CacheViews groups all cache-related default views.
func init() {
	metrics.RegisterViews(
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

		lotusmetrics.DagStorePRBytesDiscardedView,
		lotusmetrics.DagStorePRBytesRequestedView,
		lotusmetrics.DagStorePRDiscardCountView,
		lotusmetrics.DagStorePRInitCountView,
		lotusmetrics.DagStorePRSeekBackBytesView,
		lotusmetrics.DagStorePRSeekBackCountView,
		lotusmetrics.DagStorePRSeekForwardBytesView,
		lotusmetrics.DagStorePRSeekForwardCountView,
	)
}
