package proofshare

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	proofshareDurationBuckets = []float64{0.05, 0.2, 0.5, 1, 5, 15, 45} // seconds
	proofshareRetryBuckets    = []float64{0, 1, 3, 8, 20, 50, 200}

	proofshareCallTag, _ = tag.NewKey("call")
	proofshareHoldTag, _ = tag.NewKey("hold")

	proofshareQueueCount = stats.Float64(
		"psvc_proofshare_queue_count",
		"Current proofshare request queue count",
		stats.UnitDimensionless,
	)
	proofshareAdderCommits = stats.Int64(
		"psvc_proofshare_adder_commits_total",
		"Total number of successful task additions scheduled by Adder",
		stats.UnitDimensionless,
	)
	proofshareAdderHoldDecisions = stats.Int64(
		"psvc_proofshare_adder_hold_decisions_total",
		"Total number of hold decisions made by Adder",
		stats.UnitDimensionless,
	)
	proofshareNeedAsks = stats.Float64(
		"psvc_proofshare_need_asks",
		"Number of asks still needed in current Do loop iteration",
		stats.UnitDimensionless,
	)
	proofshareToRequestRemaining = stats.Float64(
		"psvc_proofshare_to_request_remaining",
		"Remaining requests to fulfill for high-water mark",
		stats.UnitDimensionless,
	)
	proofshareNewlyAdded = stats.Int64(
		"psvc_proofshare_newly_added_total",
		"Total number of new work requests inserted locally",
		stats.UnitDimensionless,
	)
	proofshareCreateAsksDuration = stats.Float64(
		"psvc_proofshare_create_asks_seconds",
		"Duration of create asks inner loop",
		stats.UnitSeconds,
	)
	proofshareDuration = stats.Float64(
		"psvc_proofshare_duration_seconds",
		"Duration of proofshare client_common operations",
		stats.UnitSeconds,
	)

	proofshareRetryCounts = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "curio_psvc_proofshare_retry_count",
		Help:    "Retry count per call in proofshare client_common operations",
		Buckets: proofshareRetryBuckets,
	}, []string{"call"})
)

func init() {
	_ = view.Register(
		&view.View{
			Measure:     proofshareQueueCount,
			Aggregation: view.LastValue(),
		},
		&view.View{
			Measure:     proofshareAdderCommits,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     proofshareAdderHoldDecisions,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{proofshareHoldTag},
		},
		&view.View{
			Measure:     proofshareNeedAsks,
			Aggregation: view.LastValue(),
		},
		&view.View{
			Measure:     proofshareToRequestRemaining,
			Aggregation: view.LastValue(),
		},
		&view.View{
			Measure:     proofshareNewlyAdded,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     proofshareCreateAsksDuration,
			Aggregation: view.Distribution(proofshareDurationBuckets...),
		},
		&view.View{
			Measure:     proofshareDuration,
			Aggregation: view.Distribution(proofshareDurationBuckets...),
			TagKeys:     []tag.Key{proofshareCallTag},
		},
	)
	_ = prometheus.Register(proofshareRetryCounts)

	initProofshareMetricRows()
}

func initProofshareMetricRows() {
	// Direct no-label Prometheus gauges and counters exported a zero sample at
	// registration time. Recording zero preserves that startup scrape surface.
	stats.Record(context.Background(),
		proofshareQueueCount.M(0),
		proofshareAdderCommits.M(0),
		proofshareNeedAsks.M(0),
		proofshareToRequestRemaining.M(0),
		proofshareNewlyAdded.M(0),
	)
}

func recordProofshareQueueCount(count int) {
	stats.Record(context.Background(), proofshareQueueCount.M(float64(count)))
}

func recordProofshareAdderCommit() {
	stats.Record(context.Background(), proofshareAdderCommits.M(1))
}

func recordProofshareAdderHoldDecision(hold string) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(proofshareHoldTag, hold),
	}, proofshareAdderHoldDecisions.M(1))
}

func recordProofshareNeedAsks(count int) {
	stats.Record(context.Background(), proofshareNeedAsks.M(float64(count)))
}

func recordProofshareToRequestRemaining(count int) {
	stats.Record(context.Background(), proofshareToRequestRemaining.M(float64(count)))
}

func recordProofshareNewlyAdded() {
	stats.Record(context.Background(), proofshareNewlyAdded.M(1))
}

func observeProofshareCreateAsksDuration(took time.Duration) {
	stats.Record(context.Background(), proofshareCreateAsksDuration.M(took.Seconds()))
}

func recordPSDuration(call string, start time.Time) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(proofshareCallTag, call),
	}, proofshareDuration.M(time.Since(start).Seconds()))
}

func recordPSRetries(call string, retries int) {
	proofshareRetryCounts.WithLabelValues(call).Observe(float64(retries))
}
