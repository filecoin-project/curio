// SPDX-License-Identifier: CCL-1.0

package proofsvc

import (
	"context"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	proofsvcCallTag, _ = tag.NewKey("call")

	clientctlBuckets = []float64{0.05, 0.2, 0.5, 1, 5} // seconds
	provictlBuckets  = []float64{0.05, 0.2, 0.5, 1, 5, 15, 45}

	clientctlDuration = stats.Float64(
		"psvc_clientctl_duration_seconds",
		"Duration of proofsvc clientctl operations",
		stats.UnitSeconds,
	)
	provictlDuration = stats.Float64(
		"psvc_provictl_duration_seconds",
		"Duration of proofsvc provider control operations",
		stats.UnitSeconds,
	)
)

func init() {
	_ = view.Register(
		&view.View{
			Measure:     clientctlDuration,
			Aggregation: view.Distribution(clientctlBuckets...),
			TagKeys:     []tag.Key{proofsvcCallTag},
		},
		&view.View{
			Measure:     provictlDuration,
			Aggregation: view.Distribution(provictlBuckets...),
			TagKeys:     []tag.Key{proofsvcCallTag},
		},
	)
}

func recordClientctlDuration(call string, start time.Time) {
	recordProofsvcDuration(clientctlDuration, call, start)
}

func recordProvictlDuration(call string, start time.Time) {
	recordProofsvcDuration(provictlDuration, call, start)
}

func recordProofsvcDuration(measure *stats.Float64Measure, call string, start time.Time) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(proofsvcCallTag, call),
	}, measure.M(time.Since(start).Seconds()))
}
