package slotmgr

import (
	"golang.org/x/xerrors"
)

type SlotMgr struct {
	Slots chan uint64
}

const maxPipelines = 2

func NewSlotMgr() *SlotMgr {
	slots := make(chan uint64, maxPipelines)
	return &SlotMgr{slots}
}

func (s *SlotMgr) Get() uint64 {
	return <-s.Slots
}

func (s *SlotMgr) Put(slot uint64) error {
	select {
	case s.Slots <- slot:
		return nil
	default:
		return xerrors.Errorf("slot list full, max %d", cap(s.Slots))
	}
}

func (s *SlotMgr) Available() int {
	return len(s.Slots)
}
