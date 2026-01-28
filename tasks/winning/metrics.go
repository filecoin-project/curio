package winning

import (
	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// Tags for winning metrics
	MinerTag, _ = tag.NewKey("miner")

	pre = "curio_mining_"

	// Histogram buckets for compute time (in seconds)
	computeTimeBuckets = []float64{0.1, 0.5, 1, 2, 5, 10, 20, 30, 60}
)

// MiningMeasures groups all mining/winning metrics.
var MiningMeasures = struct {
	WinsTotal            *stats.Int64Measure
	BlocksSubmittedTotal *stats.Int64Measure
	BlocksIncludedTotal  *stats.Int64Measure
	ComputeTime          promclient.Histogram
}{
	WinsTotal:            stats.Int64(pre+"wins_total", "Blocks won (election success).", stats.UnitDimensionless),
	BlocksSubmittedTotal: stats.Int64(pre+"blocks_submitted_total", "Blocks submitted to the network.", stats.UnitDimensionless),
	BlocksIncludedTotal:  stats.Int64(pre+"blocks_included_total", "Blocks included in the chain.", stats.UnitDimensionless),
	ComputeTime: promclient.NewHistogram(promclient.HistogramOpts{
		Name:    pre + "compute_time_seconds",
		Buckets: computeTimeBuckets,
		Help:    "Histogram of winning post compute times in seconds.",
	}),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     MiningMeasures.WinsTotal,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     MiningMeasures.BlocksSubmittedTotal,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     MiningMeasures.BlocksIncludedTotal,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
	)
	if err != nil {
		panic(err)
	}

	err = promclient.Register(MiningMeasures.ComputeTime)
	if err != nil {
		panic(err)
	}
}
