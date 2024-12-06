package slotmgr

import (
	"context"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"go.opencensus.io/stats"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var WatchInterval = 5 * time.Minute

var log = logging.Logger("slotmgr")

type slot struct {
	slotOffset uint64
	sectors    map[abi.SectorID]struct{}

	// work is true when the slot is actively used for batch sealing (P1/P2)
	// false when not is use AND when sectors in the slot are waiting for finalization
	// When work is set to false, slot.sectors should be a superset of batch refs in the DB
	// and a local process should periodically check the db for removed refs and remove them from the slot
	work bool
}

type SlotMgr struct {
	db      *harmonydb.DB
	machine string

	// in use
	slots []*slot

	lk   sync.Mutex
	cond *sync.Cond
}

func NewSlotMgr(db *harmonydb.DB, machineHostAndPort string, slotOffs []uint64) (*SlotMgr, error) {
	slots := make([]*slot, len(slotOffs))
	for i := range slots {
		slots[i] = &slot{
			sectors: map[abi.SectorID]struct{}{},
		}
	}

	var slotRefs []struct {
		SpID         int64  `db:"sp_id"`
		SectorNumber int64  `db:"sector_number"`
		PipelineSlot uint64 `db:"pipeline_slot"`
	}

	err := db.Select(context.Background(), &slotRefs, `SELECT sp_id, sector_number, pipeline_slot as count FROM batch_sector_refs WHERE machine_host_and_port = $1`, machineHostAndPort)
	if err != nil {
		return nil, xerrors.Errorf("getting slot refs: %w", err)
	}

	for _, ref := range slotRefs {
		slot, found := lo.Find(slots, func(st *slot) bool {
			return st.slotOffset == ref.PipelineSlot
		})
		if !found {
			return nil, xerrors.Errorf("slot %d not found", ref.PipelineSlot)
		}

		slot.sectors[abi.SectorID{
			Miner:  abi.ActorID(ref.SpID),
			Number: abi.SectorNumber(ref.SectorNumber),
		}] = struct{}{}
	}

	stats.Record(context.Background(), SlotMgrMeasures.SlotsAvailable.M(int64(len(slotOffs))))
	sm := &SlotMgr{
		db:      db,
		machine: machineHostAndPort,

		slots: slots,
	}
	sm.cond = sync.NewCond(&sm.lk)

	go sm.watchSlots()

	return sm, nil
}

func (s *SlotMgr) watchSlots() {
	for {
		time.Sleep(WatchInterval)
		if err := s.watchSingle(); err != nil {
			log.Errorf("watchSingle failed: %s", err)
		}
	}
}

func (s *SlotMgr) watchSingle() error {
	s.lk.Lock()
	defer s.lk.Unlock()

	for _, slt := range s.slots {
		if slt.work || len(slt.sectors) == 0 {
			// only watch slots which are NOT worked on and have sectors
			continue
		}

		var refs []struct {
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := s.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number FROM batch_sector_refs WHERE pipeline_slot = $1 AND machine_host_and_port = $2`, slt.slotOffset, s.machine)
		if err != nil {
			return xerrors.Errorf("getting refs: %w", err)
		}

		// find refs which are in the slot but not in the db
		for id := range slt.sectors {
			found := false
			for _, ref := range refs {
				if (abi.SectorID{
					Miner:  abi.ActorID(ref.SpID),
					Number: abi.SectorNumber(ref.SectorNumber),
				}) == id {
					found = true
					break
				}
			}

			if !found {
				log.Warnf("slot %d: removing local sector ref %d", slt.slotOffset, id)
				delete(slt.sectors, id)
			}
		}

		if len(slt.sectors) == 0 {
			s.cond.Signal()
		}
	}

	return nil
}

// Get allocates a slot for work. Called from a batch sealing task
func (s *SlotMgr) Get(ids []abi.SectorID) uint64 {
	s.lk.Lock()

	for {
		for _, slt := range s.slots {
			if len(slt.sectors) == 0 {
				for _, id := range ids {
					slt.sectors[id] = struct{}{}
				}
				slt.work = true

				s.lk.Unlock()

				stats.Record(context.Background(), SlotMgrMeasures.SlotsAcquired.M(1))
				stats.Record(context.Background(), SlotMgrMeasures.SlotsAvailable.M(int64(s.Available())))

				return slt.slotOffset
			}
		}

		s.cond.Wait()
	}
}

// MarkWorkDone marks a slot as no longer being actively used for batch sealing
// This is when sectors start waiting for finalization (After C1 outputs were produced)
func (s *SlotMgr) MarkWorkDone(slotOff uint64) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	sl, found := lo.Find(s.slots, func(st *slot) bool {
		return st.slotOffset == slotOff
	})
	if !found {
		return xerrors.Errorf("slot %d not found", slotOff)
	}

	sl.work = false
	return nil
}

// AbortSlot marks a slot which was used for work as immediately free
func (s *SlotMgr) AbortSlot(slotOff uint64) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	sl, found := lo.Find(s.slots, func(st *slot) bool {
		return st.slotOffset == slotOff
	})
	if !found {
		return xerrors.Errorf("slot %d not found", slotOff)
	}

	sl.sectors = map[abi.SectorID]struct{}{}
	sl.work = false
	s.cond.Signal()
	return nil
}

func (s *SlotMgr) SectorDone(ctx context.Context, slotOff uint64, id abi.SectorID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM batch_sector_refs WHERE sp_id = $1 AND sector_number = $2`, id.Miner, id.Number)
	if err != nil {
		return xerrors.Errorf("deleting batch refs: %w", err)
	}

	s.lk.Lock()
	defer s.lk.Unlock()

	sl, found := lo.Find(s.slots, func(st *slot) bool {
		return st.slotOffset == slotOff
	})
	if !found {
		return xerrors.Errorf("slot %d not found", slotOff)
	}

	delete(sl.sectors, id)
	if len(sl.sectors) == 0 {
		s.cond.Signal()
	}
	return nil
}

func (s *SlotMgr) Available() int {
	s.lk.Lock()
	defer s.lk.Unlock()

	var available int
	for _, slt := range s.slots {
		if len(slt.sectors) == 0 {
			available++
		}
	}

	return available
}
