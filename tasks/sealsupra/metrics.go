package sealsupra

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	phaseKey, _ = tag.NewKey("phase")
	pre         = "sealsupra_"
)

// SupraSealMeasures groups all SupraSeal metrics.
var SupraSealMeasures = struct {
	PhaseLockCount    *stats.Int64Measure
	PhaseWaitingCount *stats.Int64Measure
	PhaseAvgDuration  *stats.Float64Measure
}{
	PhaseLockCount:    stats.Int64(pre+"phase_lock_count", "Number of active locks in each phase", stats.UnitDimensionless),
	PhaseWaitingCount: stats.Int64(pre+"phase_waiting_count", "Number of goroutines waiting for a phase lock", stats.UnitDimensionless),
	PhaseAvgDuration:  stats.Float64(pre+"phase_avg_duration", "Average duration of each phase in seconds", stats.UnitSeconds),
}

// init registers the views for SupraSeal metrics.
func init() {
	err := view.Register(
		&view.View{
			Measure:     SupraSealMeasures.PhaseLockCount,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{phaseKey},
		},
		&view.View{
			Measure:     SupraSealMeasures.PhaseWaitingCount,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{phaseKey},
		},
		&view.View{
			Measure:     SupraSealMeasures.PhaseAvgDuration,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{phaseKey},
		},
	)
	if err != nil {
		panic(err)
	}
}
