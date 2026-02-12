package gc

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// Tags for GC metrics
	MinerTag, _    = tag.NewKey("miner")
	FileTypeTag, _ = tag.NewKey("filetype")

	pre = "curio_gc_"
)

// GCMeasures groups all GC metrics.
var GCMeasures = struct {
	SectorsMarkedTotal *stats.Int64Measure
}{
	SectorsMarkedTotal: stats.Int64(pre+"sectors_marked_total", "Sectors marked for GC.", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     GCMeasures.SectorsMarkedTotal,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{MinerTag, FileTypeTag},
		},
	)
	if err != nil {
		panic(err)
	}
}
