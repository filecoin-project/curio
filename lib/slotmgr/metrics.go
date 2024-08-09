package slotmgr

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var (
	pre = "slotmgr_"
)

// SlotMgrMeasures groups all slotmgr metrics.
var SlotMgrMeasures = struct {
	SlotsAvailable *stats.Int64Measure
	SlotsAcquired  *stats.Int64Measure
	SlotsReleased  *stats.Int64Measure
	SlotErrors     *stats.Int64Measure
}{
	SlotsAvailable: stats.Int64(pre+"slots_available", "Number of available slots.", stats.UnitDimensionless),
	SlotsAcquired:  stats.Int64(pre+"slots_acquired", "Total number of slots acquired.", stats.UnitDimensionless),
	SlotsReleased:  stats.Int64(pre+"slots_released", "Total number of slots released.", stats.UnitDimensionless),
	SlotErrors:     stats.Int64(pre+"slot_errors", "Total number of slot errors (e.g., failed to put).", stats.UnitDimensionless),
}

// init registers the views for slotmgr metrics.
func init() {
	err := view.Register(
		&view.View{
			Measure:     SlotMgrMeasures.SlotsAvailable,
			Aggregation: view.LastValue(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotsAcquired,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotsReleased,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotErrors,
			Aggregation: view.Sum(),
		},
	)
	if err != nil {
		panic(err)
	}
}
