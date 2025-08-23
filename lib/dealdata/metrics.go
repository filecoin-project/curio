package dealdata

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	kindKey, _      = tag.NewKey("kind")
)

var Measures = struct {
	DataRead    *stats.Int64Measure
}{
	DataRead:    stats.Int64("dealdata_data_read", "Number of bytes read from data URLs", stats.UnitBytes),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     Measures.DataRead,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{kindKey},
		},
	)
	if err != nil {
		panic(err)
	}
}
