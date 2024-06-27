package sealsupra

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/supraffi"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	logging "github.com/ipfs/go-log/v2"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var log = logging.Logger("batchseal")

type SupraSealNodeAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromTickets(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
}

type SupraSeal struct {
	db      *harmonydb.DB
	api     SupraSealNodeAPI
	storage *paths.Remote
	sindex  paths.SectorIndex

	pipelines int // 1 or 2
	sectors   int // sectors in a batch
	spt       abi.RegisteredSealProof

	inSDR  sync.Mutex
	outSDR sync.Mutex

	slots chan uint64
}

func NewSupraSeal(sectorSize string, batchSize, pipelines int) (*SupraSeal, error) {
	var spt abi.RegisteredSealProof
	switch sectorSize {
	case "32GiB":
		spt = abi.RegisteredSealProof_StackedDrg32GiBV1_1
	default:
		return nil, xerrors.Errorf("unsupported sector size: %s", sectorSize)
	}

	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, err
	}

	supraffi.SupraSealInit(uint64(ssize), "/tmp/supraseal.cfg")

	// Get maximum block offset (essentially the number of pages in the smallest nvme device)
	space := supraffi.GetMaxBlockOffset(uint64(ssize))

	// Get slot size (number of pages per device used for 11 layers * sector count)
	slotSize := supraffi.GetSlotSize(batchSize, uint64(ssize))

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
		sectors:   batchSize,

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

	unsealedCID := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(ssize).Unpadded())
	commd, err := commcid.CIDToDataCommitmentV1(unsealedCID)
	if err != nil {
		return false, xerrors.Errorf("getting commd: %w", err)
	}

	ticketEpochs := make([]abi.ChainEpoch, len(sectors))
	tickets := make([]abi.SealRandomness, len(sectors))
	replicaIDs := make([][32]byte, len(sectors))
	outPaths := make([]supraffi.Path, len(sectors))
	outPathIDs := make([]storiface.SectorPaths, len(sectors))
	alloc := storiface.FTSealed | storiface.FTCache

	for i, t := range sectors {
		sid := abi.SectorID{
			Miner:  abi.ActorID(t.SpID),
			Number: abi.SectorNumber(t.SectorNumber),
		}

		// cleanup any potential previous failed attempts
		if err := s.storage.Remove(ctx, sid, storiface.FTSealed, true, nil); err != nil {
			return false, xerrors.Errorf("removing sector: %w", err)
		}
		if err := s.storage.Remove(ctx, sid, storiface.FTCache, true, nil); err != nil {
			return false, xerrors.Errorf("removing sector: %w", err)
		}

		// get ticket
		maddr, err := address.NewIDAddress(uint64(t.SpID))
		if err != nil {
			return false, xerrors.Errorf("getting miner address: %w", err)
		}

		ticket, ticketEpoch, err := seal.GetTicket(ctx, s.api, maddr)
		if err != nil {
			return false, xerrors.Errorf("getting ticket: %w", err)
		}
		ticketEpochs[i] = ticketEpoch
		tickets[i] = ticket

		spt := abi.RegisteredSealProof(t.RegSealProof)
		replicaIDs[i], err = spt.ReplicaId(abi.ActorID(t.SpID), abi.SectorNumber(t.SectorNumber), ticket, commd)
		if err != nil {
			return false, xerrors.Errorf("getting replica id: %w", err)
		}

		// get output paths (before SDR so that allocating can fail early)
		sref := storiface.SectorRef{
			ID:        abi.SectorID{Miner: abi.ActorID(t.SpID), Number: abi.SectorNumber(t.SectorNumber)},
			ProofType: abi.RegisteredSealProof(t.RegSealProof),
		}

		ps, pathIDs, err := s.storage.AcquireSector(ctx, sref, storiface.FTNone, alloc, storiface.PathStorage, storiface.AcquireMove)
		if err != nil {
			return false, xerrors.Errorf("acquiring sector storage: %w", err)
		}

		outPaths[i] = supraffi.Path{
			Replica: ps.Sealed,
			Cache:   ps.Unsealed,
		}
		outPathIDs[i] = pathIDs
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

	start := time.Now()
	res := supraffi.Pc1(slot, replicaIDs, paths.ParentCacheFile, uint64(ssize))
	log.Infow("batch sdr done", "duration", time.Since(start).Truncate(time.Second), "slot", slot, "res", res, "task", taskID, "sectors", sectors)

	if res != 0 {
		return false, xerrors.Errorf("pc1 failed: %d", res)
	}

	s.inSDR.Unlock()
	s.outSDR.Lock()
	cleanup = func() {
		s.slots <- slot
		s.outSDR.Unlock()

		// Remove any files in outPaths
		for _, p := range outPaths {
			if err := os.Remove(p.Replica); err != nil {
				log.Errorf("removing replica file: %s", err)
			}
			if err := os.RemoveAll(p.Cache); err != nil {
				log.Errorf("removing cache file: %s", err)
			}
		}
	}

	start = time.Now()
	res = supraffi.Pc2(slot, s.sectors, must.One(supraffi.GenerateMultiString(outPaths)), uint64(ssize))
	log.Infow("batch tree done", "duration", time.Since(start).Truncate(time.Second), "slot", slot, "res", res, "task", taskID, "sectors", sectors)
	if res != 0 {
		return false, xerrors.Errorf("pc2 failed: %d", res)
	}

	for i, p := range outPaths {
		// in each path, write a file indicating that this is a supra-sealed sector, pipeline and slot number
		bmeta := paths.BatchMeta{
			SupraSeal:     true,
			BlockOffset:   int64(slot),
			NumInPipeline: int64(i),

			BatchSectors: s.sectors,
		}

		meta, err := json.Marshal(bmeta)
		if err != nil {
			return false, xerrors.Errorf("marshaling meta: %w", err)
		}

		if err := os.WriteFile(filepath.Join(p.Cache, paths.BatchMetaFile), meta, 0644); err != nil {
			return false, xerrors.Errorf("writing meta: %w", err)
		}
	}

	// declare sectors
	for i, ids := range outPathIDs {
		sid := abi.SectorID{
			Miner:  abi.ActorID(sectors[i].SpID),
			Number: abi.SectorNumber(sectors[i].SectorNumber),
		}
		for _, ft := range alloc.AllSet() {
			storageID := storiface.PathByType(ids, ft)
			if err := s.sindex.StorageDeclareSector(ctx, storiface.ID(storageID), sid, ft, true); err != nil {
				log.Errorf("declare sector error: %+v", err)
			}
		}
	}

	// persist success
	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		for i, sector := range sectors {
			var commr [32]byte
			if supraffi.GetCommR(commr[:], outPaths[i].Cache) {
				return false, xerrors.Errorf("getting commr failed")
			}

			sealedCID, err := commcid.ReplicaCommitmentV1ToCID(commr[:])
			if err != nil {
				return false, xerrors.Errorf("getting sealed CID: %w", err)
			}

			_, err = tx.Exec(`UPDATE sectors_sdr_pipeline SET after_sdr = TRUE, after_tree_c = TRUE, after_tree_r = TRUE, after_tree_d = TRUE,
                                ticket_epoch = $3, ticket_value = $4, tree_d_cid = $5, tree_r_cid = $6
                            WHERE sp_id = $1 AND sector_number = $2`, sector.SpID, sector.SectorNumber, ticketEpochs[i], tickets[i], unsealedCID.String(), sealedCID)
			if err != nil {
				return false, xerrors.Errorf("updating sector: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("persisting success: %w", err)
	}

	cleanup = func() {
		s.outSDR.Unlock()
		// NOTE: We're not releasing the slot yet, we keep it until sector Finalize
	}

	return true, nil
}

func (s *SupraSeal) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(s.slots) == 0 {
		return nil, nil
	}

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
