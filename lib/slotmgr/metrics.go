package slotmgr

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	pre = "slotmgr_"

	// KeySlotOffset tags metrics with the slot offset.
	KeySlotOffset, _ = tag.NewKey("slot_offset")
)

// SlotMgrMeasures groups the high-level slotmgr metrics.
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

// SlotMgrSlotMeasures groups per-slot metrics.
var SlotMgrSlotMeasures = struct {
	SlotInUse       *stats.Int64Measure
	SlotSectorCount *stats.Int64Measure
}{
	SlotInUse:       stats.Int64(pre+"slot_in_use", "Slot actively in use (batch sealing). 1=in use, 0=not in use", stats.UnitDimensionless),
	SlotSectorCount: stats.Int64(pre+"slot_sector_count", "Number of sectors in the slot", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     SlotMgrMeasures.SlotsAvailable,
			Description: "Number of available slots",
			Aggregation: view.LastValue(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotsAcquired,
			Description: "Total number of slots acquired",
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotsReleased,
			Description: "Total number of slots released",
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     SlotMgrMeasures.SlotErrors,
			Description: "Total number of slot errors",
			Aggregation: view.Sum(),
		},

		// Register per-slot metrics
		&view.View{
			Name:        pre + "slot_in_use",
			Measure:     SlotMgrSlotMeasures.SlotInUse,
			Description: "Slot is in use (1) or not (0)",
			TagKeys:     []tag.Key{KeySlotOffset},
			Aggregation: view.LastValue(),
		},
		&view.View{
			Name:        pre + "slot_sector_count",
			Measure:     SlotMgrSlotMeasures.SlotSectorCount,
			Description: "Number of sectors in a slot",
			TagKeys:     []tag.Key{KeySlotOffset},
			Aggregation: view.LastValue(),
		},
	)
	if err != nil {
		panic(err)
	}
}
