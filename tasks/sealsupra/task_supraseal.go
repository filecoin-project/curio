package sealsupra

import (
	"context"
	"fmt"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/supraffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"
	"os"
	"sync"
)

var parentCache string

func init() {
	if os.Getenv("FIL_PROOFS_PARENT_CACHE") != "" {
		parentCache = os.Getenv("FIL_PROOFS_PARENT_CACHE")
	} else {
		parentCache = "/var/tmp/filecoin-parents"
	}
}

type SupraSeal struct {
	db *harmonydb.DB

	pipelines int // 1 or 2
	sectors   int // sectors in a batch
	spt       abi.RegisteredSealProof

	inSDR  sync.Mutex
	outSDR sync.Mutex

	slots chan uint64
}

func NewSupraSeal(spt abi.RegisteredSealProof) (*SupraSeal, error) {
	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, err
	}

	sectors := 32 // TODO: get from config
	pipelines := 2

	supraffi.SupraSealInit(uint64(ssize), "/tmp/supraseal.cfg")

	// Get maximum block offset (essentially the number of pages in the smallest nvme device)
	space := supraffi.GetMaxBlockOffset(uint64(ssize))

	// Get slot size (number of pages per device used for 11 layers * sector count)
	slotSize := supraffi.GetSlotSize(sectors, uint64(ssize))

	maxPipelines := space / slotSize
	if maxPipelines < slotSize*uint64(pipelines) {
		return nil, xerrors.Errorf("not enough space for %d pipelines, only %d pages available, want %d pages", pipelines, space, slotSize*uint64(pipelines))
	}

	slots := make(chan uint64, pipelines)
	for i := 0; i < pipelines; i++ {
		slots <- slotSize * uint64(i)
	}

	return &SupraSeal{
		// TODO: get pipelines and sectors from config
		spt: spt,

		pipelines: pipelines,
		sectors:   sectors,

		slots: slots,
	}, nil
}

func (s *SupraSeal) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`

		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = s.db.Select(ctx, &sectors, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline WHERE task_id_sdr = $1 AND task_id_tree_r = $1 AND task_id_tree_c = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting sector params: %w", err)
	}

	if len(sectors) != s.sectors {
		return false, xerrors.Errorf("not enough sectors to fill a batch")
	}

	ssize, err := s.spt.SectorSize()
	if err != nil {
		return false, err
	}

	replicaIDs := make([][32]byte, len(sectors))
	for i, t := range sectors {
		spt := abi.RegisteredSealProof(t.RegSealProof)
		replicaIDs[i], err = spt.ReplicaId(abi.ActorID(t.SpID), abi.SectorNumber(t.SectorNumber), ticket, commd)
		if err != nil {
			return false, xerrors.Errorf("getting replica id: %w", err)
		}
	}

	s.inSDR.Lock()
	slot := <-s.slots

	cleanup := func() {
		s.slots <- slot
		s.inSDR.Unlock()
	}
	defer func() {
		cleanup()
	}()

	supraffi.Pc1(slot, replicaIDs, parentCache, uint64(ssize))

	s.inSDR.Unlock()
	cleanup = func() {
		s.slots <- slot
	}
}

func (s *SupraSeal) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

var ssizeToName = map[abi.SectorSize]string{
	abi.SectorSize(2 << 10):   "2K",
	abi.SectorSize(8 << 20):   "8M",
	abi.SectorSize(512 << 20): "512M",
	abi.SectorSize(32 << 30):  "32G",
	abi.SectorSize(64 << 30):  "64G",
}

func (s *SupraSeal) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  s.pipelines,
		Name: fmt.Sprintf("Batch%d-%s", s.sectors, ssizeToName[must.One(s.spt.SectorSize())]),
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 1,
			Ram: 1 << 20,
		},
		MaxFailures: 4,
		IAmBored:    nil, // TODO
	}
}

func (s *SupraSeal) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
}

func (s *SupraSeal) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		// claim [sectors] pipeline entries
		var sectors []struct {
			SpID         int64 `db:"sp_id"`
			SectorNumber int64 `db:"sector_number"`
		}

		err := tx.Select(&sectors, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline WHERE after_sdr = FALSE AND task_id_sdr IS NULL LIMIT $1`, s.sectors)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		if len(sectors) != s.sectors {
			// not enough sectors to fill a batch
			return false, nil
		}

		// assign to pipeline entries, set task_id_sdr, task_id_tree_r, task_id_tree_c
		for _, t := range sectors {
			_, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_sdr = $1, task_id_tree_r = $1, task_id_tree_c = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}
		}

		return true, nil
	})

	return nil
}

var _ harmonytask.TaskInterface = &SupraSeal{}
