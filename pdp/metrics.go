package pdp

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// Tag keys for metrics
var (
	// RequesterDomain tracks the domain of the requester
	RequesterDomainKey = tag.MustNewKey("requester_domain")
	
	// ServiceLabel tracks which PDP service is handling the request
	ServiceLabelKey = tag.MustNewKey("service_label")
	
	// PieceSizeCategory tracks the size category of the piece
	PieceSizeCategoryKey = tag.MustNewKey("piece_size_category")
	
	// ResponseStatusKey tracks HTTP response status codes
	ResponseStatusKey = tag.MustNewKey("response_status")
)

// Distribution for request duration in milliseconds
var defaultMillisecondsDistribution = view.Distribution(
	1,      // 1 millisecond
	5,      // 5 milliseconds
	10,     // 10 milliseconds
	25,     // 25 milliseconds
	50,     // 50 milliseconds
	100,    // 100 milliseconds
	250,    // 250 milliseconds
	500,    // 500 milliseconds
	1000,   // 1 second
	2500,   // 2.5 seconds
	5000,   // 5 seconds
	10000,  // 10 seconds
)

// Size distribution for piece sizes in bytes
var pieceSizeDistribution = view.Distribution(
	1024,        // 1 KB
	10240,       // 10 KB
	102400,      // 100 KB
	1048576,     // 1 MB
	10485760,    // 10 MB
	104857600,   // 100 MB
	1073741824,  // 1 GB
	10737418240, // 10 GB
)

// Metrics measures
var (
	// PDP retrieval request metrics
	PDPRetrievalRequestCount = stats.Int64(
		"pdp/retrieval_request_count",
		"Counter of PDP retrieval requests",
		stats.UnitDimensionless,
	)
	
	PDPRetrievalRequestDuration = stats.Float64(
		"pdp/retrieval_request_duration_ms",
		"Time spent processing PDP retrieval requests",
		stats.UnitMilliseconds,
	)
	
	PDPRetrievalSuccessCount = stats.Int64(
		"pdp/retrieval_success_count",
		"Counter of successful PDP retrieval requests",
		stats.UnitDimensionless,
	)
	
	PDPRetrievalErrorCount = stats.Int64(
		"pdp/retrieval_error_count",
		"Counter of failed PDP retrieval requests",
		stats.UnitDimensionless,
	)
	
	PDPRetrievalNotFoundCount = stats.Int64(
		"pdp/retrieval_not_found_count",
		"Counter of PDP retrieval requests for non-existent pieces",
		stats.UnitDimensionless,
	)
	
	PDPRetrievalPieceSize = stats.Int64(
		"pdp/retrieval_piece_size_bytes",
		"Size of pieces in retrieval requests",
		stats.UnitBytes,
	)
	
	// Service-specific metrics
	PDPServiceRequestCount = stats.Int64(
		"pdp/service_request_count",
		"Counter of requests per PDP service",
		stats.UnitDimensionless,
	)
	
	// Domain-specific metrics
	PDPDomainRequestCount = stats.Int64(
		"pdp/domain_request_count",
		"Counter of requests per requester domain",
		stats.UnitDimensionless,
	)
)

// Metric views
var (
	PDPRetrievalRequestCountView = &view.View{
		Name:        "pdp_retrieval_request_count",
		Measure:     PDPRetrievalRequestCount,
		Description: "Number of PDP retrieval requests by domain and service",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey, ResponseStatusKey},
	}
	
	PDPRetrievalRequestDurationView = &view.View{
		Name:        "pdp_retrieval_request_duration",
		Measure:     PDPRetrievalRequestDuration,
		Description: "Duration of PDP retrieval requests",
		Aggregation: defaultMillisecondsDistribution,
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey, ResponseStatusKey},
	}
	
	PDPRetrievalSuccessCountView = &view.View{
		Name:        "pdp_retrieval_success_count",
		Measure:     PDPRetrievalSuccessCount,
		Description: "Number of successful PDP retrieval requests",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey},
	}
	
	PDPRetrievalErrorCountView = &view.View{
		Name:        "pdp_retrieval_error_count",
		Measure:     PDPRetrievalErrorCount,
		Description: "Number of failed PDP retrieval requests",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey, ResponseStatusKey},
	}
	
	PDPRetrievalNotFoundCountView = &view.View{
		Name:        "pdp_retrieval_not_found_count",
		Measure:     PDPRetrievalNotFoundCount,
		Description: "Number of PDP retrieval requests for non-existent pieces",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey},
	}
	
	PDPRetrievalPieceSizeView = &view.View{
		Name:        "pdp_retrieval_piece_size",
		Measure:     PDPRetrievalPieceSize,
		Description: "Size distribution of pieces in retrieval requests",
		Aggregation: pieceSizeDistribution,
		TagKeys:     []tag.Key{RequesterDomainKey, ServiceLabelKey, PieceSizeCategoryKey},
	}
	
	PDPServiceRequestCountView = &view.View{
		Name:        "pdp_service_request_count",
		Measure:     PDPServiceRequestCount,
		Description: "Number of requests per PDP service",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{ServiceLabelKey},
	}
	
	PDPDomainRequestCountView = &view.View{
		Name:        "pdp_domain_request_count",
		Measure:     PDPDomainRequestCount,
		Description: "Number of requests per requester domain",
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{RequesterDomainKey},
	}
)

// Register all PDP metrics views
func RegisterPDPMetrics() error {
	return view.Register(
		PDPRetrievalRequestCountView,
		PDPRetrievalRequestDurationView,
		PDPRetrievalSuccessCountView,
		PDPRetrievalErrorCountView,
		PDPRetrievalNotFoundCountView,
		PDPRetrievalPieceSizeView,
		PDPServiceRequestCountView,
		PDPDomainRequestCountView,
	)
}

// Helper functions for categorizing piece sizes
func categorizePieceSize(size int64) string {
	switch {
	case size < 1024:
		return "tiny"
	case size < 1024*1024:
		return "small"
	case size < 100*1024*1024:
		return "medium"
	case size < 1024*1024*1024:
		return "large"
	default:
		return "huge"
	}
}

// Helper function to extract domain from HTTP request
func extractDomainFromRequest(host string) string {
	if host == "" {
		return "unknown"
	}
	// Extract just the domain part, removing port if present
	if colonIndex := len(host) - 1; colonIndex >= 0 {
		for i := len(host) - 1; i >= 0; i-- {
			if host[i] == ':' {
				return host[:i]
			}
		}
	}
	return host
}