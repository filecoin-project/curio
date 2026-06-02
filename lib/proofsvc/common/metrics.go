package common

import (
	"context"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	l1OpCallTag, _ = tag.NewKey("call")

	l1OpBuckets = []float64{0.05, 0.2, 0.5, 1, 5} // seconds

	l1OpDuration = stats.Float64(
		"psvc_l1ops_duration_seconds",
		"Duration of L1 operations",
		stats.UnitSeconds,
	)
)

func init() {
	_ = view.Register(&view.View{
		Measure:     l1OpDuration,
		Aggregation: view.Distribution(l1OpBuckets...),
		TagKeys:     []tag.Key{l1OpCallTag},
	})
}

func recordL1OpDuration(call string, start time.Time) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(l1OpCallTag, call),
	}, l1OpDuration.M(time.Since(start).Seconds()))
}
