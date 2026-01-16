package robusthttp

import (
	"context"
	"sync/atomic"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var (
	// Measures
	metricBytesRead       = stats.Int64("robusthttp_bytes_read", "Total bytes delivered by robusthttp readers", stats.UnitBytes)
	metricRequestsStarted = stats.Int64("robusthttp_requests_started", "Number of robusthttp logical requests started", stats.UnitDimensionless)
	metricRetries         = stats.Int64("robusthttp_retries", "Total number of robusthttp retries across requests", stats.UnitDimensionless)
	metricReadErrors      = stats.Int64("robusthttp_read_errors", "Total number of robusthttp read/request errors (non-EOF)", stats.UnitDimensionless)
	metricReadFailures    = stats.Int64("robusthttp_read_failures", "Number of robusthttp requests that failed after retries", stats.UnitDimensionless)
	metricActiveTransfers = stats.Int64("robusthttp_active_transfers", "Current number of active robusthttp transfers", stats.UnitDimensionless)

	activeTransfers atomic.Int64
)

func init() {
	_ = view.Register(
		&view.View{Measure: metricBytesRead, Aggregation: view.Sum()},
		&view.View{Measure: metricRequestsStarted, Aggregation: view.Sum()},
		&view.View{Measure: metricRetries, Aggregation: view.Sum()},
		&view.View{Measure: metricReadErrors, Aggregation: view.Sum()},
		&view.View{Measure: metricReadFailures, Aggregation: view.Sum()},
		&view.View{Measure: metricActiveTransfers, Aggregation: view.LastValue()},
	)
}

// Lightweight helpers to record metrics with minimal overhead and no tags to avoid
// high cardinality at very high call rates.

func recordRequestStarted() {
	stats.Record(context.Background(), metricRequestsStarted.M(1))
}

func recordRequestClosed(bytesRead, retries, errors int64) {
	if bytesRead > 0 {
		stats.Record(context.Background(), metricBytesRead.M(bytesRead))
	}
	if retries > 0 {
		stats.Record(context.Background(), metricRetries.M(retries))
	}
	if errors > 0 {
		stats.Record(context.Background(), metricReadErrors.M(errors))
	}
}

func recordReadFailure() {
	stats.Record(context.Background(), metricReadFailures.M(1))
}

func incActiveTransfers() {
	val := activeTransfers.Add(1)
	stats.Record(context.Background(), metricActiveTransfers.M(val))
}

func decActiveTransfers() {
	val := activeTransfers.Add(-1)
	stats.Record(context.Background(), metricActiveTransfers.M(val))
}
