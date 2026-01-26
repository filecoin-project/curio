package seal

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// Tags for seal metrics
	MinerTag, _ = tag.NewKey("miner")
	StageTag, _ = tag.NewKey("stage")

	pre = "curio_seal_"
)

// SealMeasures groups all seal pipeline metrics.
var SealMeasures = struct {
	SDRCompleted         *stats.Int64Measure
	TreeDCompleted       *stats.Int64Measure
	TreeRCCompleted      *stats.Int64Measure
	SynthCompleted       *stats.Int64Measure
	PoRepCompleted       *stats.Int64Measure
	FinalizeCompleted    *stats.Int64Measure
	MoveStorageCompleted *stats.Int64Measure
	PrecommitSubmitted   *stats.Int64Measure
	CommitSubmitted      *stats.Int64Measure
	Failed               *stats.Int64Measure
}{
	SDRCompleted:         stats.Int64(pre+"sdr_completed_total", "SDR computations completed.", stats.UnitDimensionless),
	TreeDCompleted:       stats.Int64(pre+"treed_completed_total", "Tree D computations completed.", stats.UnitDimensionless),
	TreeRCCompleted:      stats.Int64(pre+"treerc_completed_total", "Tree R/C computations completed.", stats.UnitDimensionless),
	SynthCompleted:       stats.Int64(pre+"synth_completed_total", "Synthetic proofs completed.", stats.UnitDimensionless),
	PoRepCompleted:       stats.Int64(pre+"porep_completed_total", "PoRep computations completed.", stats.UnitDimensionless),
	FinalizeCompleted:    stats.Int64(pre+"finalize_completed_total", "Finalizations completed.", stats.UnitDimensionless),
	MoveStorageCompleted: stats.Int64(pre+"movestorage_completed_total", "Move storage operations completed.", stats.UnitDimensionless),
	PrecommitSubmitted:   stats.Int64(pre+"precommit_submitted_total", "Precommit messages submitted.", stats.UnitDimensionless),
	CommitSubmitted:      stats.Int64(pre+"commit_submitted_total", "Commit messages submitted.", stats.UnitDimensionless),
	Failed:               stats.Int64(pre+"failed_total", "Failed sectors by stage.", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     SealMeasures.SDRCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.TreeDCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.TreeRCCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.SynthCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.PoRepCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.FinalizeCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.MoveStorageCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.PrecommitSubmitted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.CommitSubmitted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SealMeasures.Failed,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag, StageTag},
		},
	)
	if err != nil {
		panic(err)
	}
}
