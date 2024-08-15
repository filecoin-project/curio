package slotmgr

import (
	"context"

	"go.opencensus.io/stats"
	"golang.org/x/xerrors"
)

type SlotMgr struct {
	Slots chan uint64
}

const maxPipelines = 2

func NewSlotMgr() *SlotMgr {
	slots := make(chan uint64, maxPipelines)
	stats.Record(context.Background(), SlotMgrMeasures.SlotsAvailable.M(int64(maxPipelines)))
	return &SlotMgr{slots}
}

func (s *SlotMgr) Get() uint64 {
	slot := <-s.Slots
	stats.Record(context.Background(), SlotMgrMeasures.SlotsAcquired.M(1))
	stats.Record(context.Background(), SlotMgrMeasures.SlotsAvailable.M(int64(s.Available())))
	return slot
}

func (s *SlotMgr) Put(slot uint64) error {
	select {
	case s.Slots <- slot:
		stats.Record(context.Background(), SlotMgrMeasures.SlotsReleased.M(1))
		stats.Record(context.Background(), SlotMgrMeasures.SlotsAvailable.M(int64(s.Available())))
		return nil
	default:
		stats.Record(context.Background(), SlotMgrMeasures.SlotErrors.M(1))
		return xerrors.Errorf("slot list full, max %d", cap(s.Slots))
	}
}

func (s *SlotMgr) Available() int {
	return len(s.Slots)
}
