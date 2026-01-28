package snap

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// Tags for snap metrics
	MinerTag, _ = tag.NewKey("miner")

	pre = "curio_snap_"
)

// SnapMeasures groups all snap pipeline metrics.
var SnapMeasures = struct {
	EncodeCompleted      *stats.Int64Measure
	ProveCompleted       *stats.Int64Measure
	SubmitCompleted      *stats.Int64Measure
	MoveStorageCompleted *stats.Int64Measure
}{
	EncodeCompleted:      stats.Int64(pre+"encode_completed_total", "Snap encodes completed.", stats.UnitDimensionless),
	ProveCompleted:       stats.Int64(pre+"prove_completed_total", "Snap proves completed.", stats.UnitDimensionless),
	SubmitCompleted:      stats.Int64(pre+"submit_completed_total", "Snap submissions completed.", stats.UnitDimensionless),
	MoveStorageCompleted: stats.Int64(pre+"movestorage_completed_total", "Snap move storage operations completed.", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     SnapMeasures.EncodeCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SnapMeasures.ProveCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SnapMeasures.SubmitCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
		&view.View{
			Measure:     SnapMeasures.MoveStorageCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag},
		},
	)
	if err != nil {
		panic(err)
	}
}
