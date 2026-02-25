package sealsupra

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/snadrus/must"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/hugepageutil"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/slotmgr"
	storiface "github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/chain/types"
)

const suprasealConfigEnv = "SUPRASEAL_CONFIG"

var log = logging.Logger("batchseal")

type SupraSealNodeAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromTickets(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
	StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error)
}

type SupraSeal struct {
	db      *harmonydb.DB
	api     SupraSealNodeAPI
	storage *paths.Remote
	sindex  paths.SectorIndex
	sc      *ffi.SealCalls

	pipelines int // 1 or 2
	sectors   int // sectors in a batch
	spt       abi.RegisteredSealProof

	inSDR  *pipelinePhase // Phase 1
	outSDR *pipelinePhase // Phase 2

	slots *slotmgr.SlotMgr
}

type P2Active func() bool

func NewSupraSeal(sectorSize string, batchSize, pipelines int, dualHashers bool, nvmeDevices []string, machineHostAndPort string,
	db *harmonydb.DB, api SupraSealNodeAPI, storage *paths.Remote, sindex paths.SectorIndex, sc *ffi.SealCalls) (*SupraSeal, *slotmgr.SlotMgr, P2Active, error) {

	// Check CPU features before initializing supraseal
	// supraseal's PC1 (sha_ext_mbx2) requires:
	// - Intel SHA Extensions (SHA-NI) for sha256rnds2, sha256msg1, sha256msg2
	// - SSE2, SSSE3, SSE4.1 for supporting SIMD operations
	if !supraffi.CanRunSupraSealPC1() {
		cpuInfo := supraffi.CPUFeaturesSummary()
		return nil, nil, nil, xerrors.Errorf("CPU does not support supraseal requirements (SHA-NI + SSE4.1 required). "+
			"Detected features: %s. "+
			"Use single-sector sealing with filecoin-ffi instead, or upgrade to AMD Zen1+ / Intel Ice Lake+ CPU", cpuInfo)
	}

	var spt abi.RegisteredSealProof
	switch sectorSize {
	case "32GiB":
		spt = abi.RegisteredSealProof_StackedDrg32GiBV1_1
	default:
		return nil, nil, nil, xerrors.Errorf("unsupported sector size: %s", sectorSize)
	}

	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, nil, nil, err
	}

	log.Infow("start supraseal init", "cpu_features", supraffi.CPUFeaturesSummary())

	// Automatically setup SPDK (configure hugepages and bind NVMe devices)
	if os.Getenv("DISABLE_SPDK_SETUP") != "" {
		log.Infow("SPDK setup disabled by environment variable")
	} else {
		log.Infow("checking and setting up SPDK for supraseal")
		if err := supraffi.CheckAndSetupSPDK(36, 36); err != nil {
			return nil, nil, nil, xerrors.Errorf("SPDK setup failed: %w. Please ensure you have:\n"+
				"1. Configured 1GB hugepages (add 'hugepages=36 default_hugepagesz=1G hugepagesz=1G' to /etc/default/grub)\n"+
				"2. Raw NVMe devices available (no filesystems on them)\n"+
				"3. Root/sudo access for SPDK setup", err)
		}
	}

	var configFile string
	if configFile = os.Getenv(suprasealConfigEnv); configFile == "" {
		// not set from env (should be the case in most cases), auto-generate a config

		var cstr string
		cstr, nvmeDevices, err = GenerateSupraSealConfigString(dualHashers, batchSize, nvmeDevices)
		if err != nil {
			return nil, nil, nil, xerrors.Errorf("generating supraseal config: %w", err)
		}

		log.Infow("nvme devices", "nvmeDevices", nvmeDevices)
		if len(nvmeDevices) == 0 {
			return nil, nil, nil, xerrors.Errorf("no nvme devices found. Please ensure you have raw NVMe devices (without filesystems) available")
		}

		cfgFile, err := os.CreateTemp("", "supraseal-config-*.cfg")
		if err != nil {
			return nil, nil, nil, xerrors.Errorf("creating temp file: %w", err)
		}

		if _, err := cfgFile.WriteString(cstr); err != nil {
			return nil, nil, nil, xerrors.Errorf("writing temp file: %w", err)
		}

		configFile = cfgFile.Name()
		if err := cfgFile.Close(); err != nil {
			return nil, nil, nil, xerrors.Errorf("closing temp file: %w", err)
		}

		log.Infow("generated supraseal config", "config", cstr, "file", configFile)
	}

	supraffi.SupraSealInit(uint64(ssize), configFile)
	log.Infow("supraseal init done")

	{
		hp, err := supraffi.GetHealthInfo()
		if err != nil {
			return nil, nil, nil, xerrors.Errorf("get health page: %w", err)
		}

		log.Infow("nvme health page", "hp", hp)
	}

	// Initialize previous health infos slice
	prevHealthInfos := make([]supraffi.HealthInfo, len(nvmeDevices))

	go func() {
		const intervalSeconds = 30
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			healthInfos, err := supraffi.GetHealthInfo()
			if err != nil {
				log.Errorw("health page get error", "error", err)
				continue
			}

			for i, hi := range healthInfos {
				if i >= len(nvmeDevices) {
					log.Warnw("More health info entries than nvme devices", "index", i)
					break
				}
				deviceName := nvmeDevices[i]

				ctx, err := tag.New(
					context.Background(),
					tag.Insert(nvmeDeviceKey, deviceName),
				)
				if err != nil {
					log.Errorw("Failed to create context with tags", "error", err)
					continue
				}

				// Record the metrics
				stats.Record(ctx, SupraSealMeasures.NVMeTemperature.M(hi.Temperature))
				stats.Record(ctx, SupraSealMeasures.NVMeAvailableSpare.M(int64(hi.AvailableSpare)))
				stats.Record(ctx, SupraSealMeasures.NVMePercentageUsed.M(int64(hi.PercentageUsed)))
				stats.Record(ctx, SupraSealMeasures.NVMePowerCycles.M(int64(hi.PowerCycles)))
				stats.Record(ctx, SupraSealMeasures.NVMePowerOnHours.M(hi.PowerOnHours.Hours()))
				stats.Record(ctx, SupraSealMeasures.NVMeUnsafeShutdowns.M(int64(hi.UnsafeShutdowns)))
				stats.Record(ctx, SupraSealMeasures.NVMeMediaErrors.M(int64(hi.MediaErrors)))
				stats.Record(ctx, SupraSealMeasures.NVMeErrorLogEntries.M(int64(hi.ErrorLogEntries)))
				stats.Record(ctx, SupraSealMeasures.NVMeCriticalWarning.M(int64(hi.CriticalWarning)))

				// For counters, compute difference from previous values
				if prevHealthInfos[i].DataUnitsRead != 0 {
					dataUnitsReadBytes := int64((hi.DataUnitsRead - prevHealthInfos[i].DataUnitsRead) * 512_000)
					dataUnitsWrittenBytes := int64((hi.DataUnitsWritten - prevHealthInfos[i].DataUnitsWritten) * 512_000)
					hostReadCommands := int64(hi.HostReadCommands - prevHealthInfos[i].HostReadCommands)
					hostWriteCommands := int64(hi.HostWriteCommands - prevHealthInfos[i].HostWriteCommands)

					// Record the diffs and computed metrics
					stats.Record(ctx, SupraSealMeasures.NVMeBytesRead.M(dataUnitsReadBytes))
					stats.Record(ctx, SupraSealMeasures.NVMeBytesWritten.M(dataUnitsWrittenBytes))
					stats.Record(ctx, SupraSealMeasures.NVMeReadIO.M(hostReadCommands))
					stats.Record(ctx, SupraSealMeasures.NVMeWriteIO.M(hostWriteCommands))
				}

				// Update previous health info
				prevHealthInfos[i] = hi
			}
		}
	}()

	// Get maximum block offset (essentially the number of pages in the smallest nvme device)
	space := supraffi.GetMaxBlockOffset(uint64(ssize))

	// Get slot size (number of pages per device used for 11 layers * sector count)
	slotSize := supraffi.GetSlotSize(batchSize, uint64(ssize))

	maxPipelines := space / slotSize
	if maxPipelines < uint64(pipelines) {
		return nil, nil, nil, xerrors.Errorf("not enough space for %d pipelines (can do %d), only %d pages available, want %d (slot size %d) pages", pipelines, maxPipelines, space, slotSize*uint64(pipelines), slotSize)
	}

	var slotOffs []uint64
	for i := 0; i < pipelines; i++ {
		slot := slotSize * uint64(i)
		log.Infow("batch slot", "slot", slot, "machine", machineHostAndPort)
		slotOffs = append(slotOffs, slot)
	}

	slots, err := slotmgr.NewSlotMgr(db, machineHostAndPort, slotOffs)
	if err != nil {
		return nil, nil, nil, xerrors.Errorf("creating slot manager: %w", err)
	}

	ssl := &SupraSeal{
		db:      db,
		api:     api,
		storage: storage,
		sindex:  sindex,
		sc:      sc,

		spt:       spt,
		pipelines: pipelines,
		sectors:   batchSize,

		inSDR:  &pipelinePhase{phaseNum: 1},
		outSDR: &pipelinePhase{phaseNum: 2},

		slots: slots,
	}

	return ssl, slots, ssl.outSDR.IsInPhase(), nil
}

func (s *SupraSeal) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var sectors []struct {
		SpID         int64 `db:"sp_id"`
		SectorNumber int64 `db:"sector_number"`

		RegSealProof int64 `db:"reg_seal_proof"`
	}

	err = s.db.Select(ctx, &sectors, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_sdr = $1 AND task_id_tree_r = $1 AND task_id_tree_c = $1 AND task_id_tree_d = $1`, taskID)
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
	sectorsIDs := make([]abi.SectorID, 0, len(sectors))

	releaseStorage := func() {}
	defer func() {
		releaseStorage()
	}()

	for i, t := range sectors {
		sid := abi.SectorID{
			Miner:  abi.ActorID(t.SpID),
			Number: abi.SectorNumber(t.SectorNumber),
		}
		sectorsIDs = append(sectorsIDs, sid)

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

		ctx = context.WithValue(ctx, paths.SpaceUseKey, paths.SpaceUseFunc(SupraSpaceUse))

		ps, pathIDs, release, err := s.sc.Sectors.AcquireSector(ctx, &taskID, sref, storiface.FTNone, alloc, storiface.PathSealing)
		if err != nil {
			return false, xerrors.Errorf("acquiring sector storage: %w", err)
		}
		prevReleaseStorage := releaseStorage
		releaseStorage = func() {
			release()
			prevReleaseStorage()
		}

		outPaths[i] = supraffi.Path{
			Replica: ps.Sealed,
			Cache:   ps.Cache,
		}
		outPathIDs[i] = pathIDs
	}

	s.inSDR.Lock()
	slot := s.slots.Get(sectorsIDs)

	cleanup := func() {
		perr := s.slots.AbortSlot(slot)
		if perr != nil {
			log.Errorf("putting slot back: %s", err)
		}
		s.inSDR.Unlock()
	}
	defer func() {
		cleanup()
	}()

	parentPath, err := paths.ParentsForProof(s.spt)
	if err != nil {
		return false, xerrors.Errorf("getting parent path: %w", err)
	}

	start := time.Now() //nolint:staticcheck
	res := supraffi.Pc1(slot, replicaIDs, parentPath, uint64(ssize))
	duration := time.Since(start).Truncate(time.Second)
	log.Infow("batch sdr done", "duration", duration, "slot", slot, "res", res, "task", taskID, "sectors", sectors, "spt", sectors[0].RegSealProof, "replicaIDs", replicaIDs)

	if res != 0 {
		return false, xerrors.Errorf("pc1 failed: %d", res)
	}

	s.inSDR.Unlock()
	s.outSDR.Lock()
	cleanup = func() {
		perr := s.slots.AbortSlot(slot)
		if perr != nil {
			log.Errorf("putting slot back: %s", err)
		}
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

	log.Infow("batch tree start", "slot", slot, "task", taskID, "sectors", sectors, "pstring", hex.EncodeToString([]byte(must.One(supraffi.GenerateMultiString(outPaths)))))

	start2 := time.Now()
	res = supraffi.Pc2(slot, s.sectors, must.One(supraffi.GenerateMultiString(outPaths)), uint64(ssize))
	log.Infow("batch tree done", "duration", time.Since(start2).Truncate(time.Second), "slot", slot, "res", res, "task", taskID, "sectors", sectors)
	if res != 0 {
		return false, xerrors.Errorf("pc2 failed: %d", res)
	}

	for i, p := range outPaths {
		// in each path, write a file indicating that this is a supra-sealed sector, pipeline and slot number
		bmeta := paths.BatchMeta{
			SupraSeal:     true,
			BlockOffset:   slot,
			NumInPipeline: i,

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

	if !stillOwned() {
		return false, xerrors.Errorf("task is no longer owned!")
	}

	// persist success
	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// get machine id
		var ownedBy []struct {
			HostAndPort string `db:"host_and_port"`
		}

		err = tx.Select(&ownedBy, `SELECT hm.host_and_port FROM harmony_task INNER JOIN harmony_machines hm on harmony_task.owner_id = hm.id WHERE harmony_task.id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("getting machine id: %w", err)
		}

		if len(ownedBy) != 1 {
			return false, xerrors.Errorf("no machine found for task %d", taskID)
		}

		for i, sector := range sectors {
			var commr [32]byte
			if !supraffi.GetCommR(commr[:], outPaths[i].Cache) {
				return false, xerrors.Errorf("getting commr failed")
			}

			sealedCID, err := commcid.ReplicaCommitmentV1ToCID(commr[:])
			if err != nil {
				return false, xerrors.Errorf("getting sealed CID: %w", err)
			}

			_, err = tx.Exec(`UPDATE sectors_sdr_pipeline SET after_sdr = TRUE, after_tree_c = TRUE, after_tree_r = TRUE, after_tree_d = TRUE, after_synth = TRUE,
                                ticket_epoch = $3, ticket_value = $4, tree_d_cid = $5, tree_r_cid = $6, task_id_sdr = NULL, task_id_tree_r = NULL, task_id_tree_c = NULL, task_id_tree_d = NULL
                            WHERE sp_id = $1 AND sector_number = $2`, sector.SpID, sector.SectorNumber, ticketEpochs[i], tickets[i], unsealedCID.String(), sealedCID)
			if err != nil {
				return false, xerrors.Errorf("updating sector: %w", err)
			}

			// insert batch refs
			_, err = tx.Exec(`INSERT INTO batch_sector_refs (sp_id, sector_number, machine_host_and_port, pipeline_slot)
    						  VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, sector.SpID, sector.SectorNumber, ownedBy[0].HostAndPort, slot)
			if err != nil {
				return false, xerrors.Errorf("inserting batch refs: %w", err)
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

	if err := s.slots.MarkWorkDone(slot); err != nil {
		return true, xerrors.Errorf("marking work done: %w", err)
	}

	return true, nil
}

func (s *SupraSeal) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if s.slots.Available() == 0 {
		return []harmonytask.TaskID{}, nil
	}

	// check if we have enough huge pages available
	// sysctl vm.nr_hugepages should be >= 36 for 32G sectors
	if err := hugepageutil.CheckHugePages(36); err != nil {
		log.Warnw("huge pages check failed, try 'sudo sysctl -w vm.nr_hugepages=36' and make sure your system uses 1G huge pages", "err", err)
		return []harmonytask.TaskID{}, nil
	}

	return ids, nil
}

var ssizeToName = map[abi.SectorSize]string{
	abi.SectorSize(2 << 10):   "2K",
	abi.SectorSize(8 << 20):   "8M",
	abi.SectorSize(512 << 20): "512M",
	abi.SectorSize(32 << 30):  "32G",
	abi.SectorSize(64 << 30):  "64G",
}

func (s *SupraSeal) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size
	if seal.IsDevnet {
		ssize = abi.SectorSize(2 << 20)
	}

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(s.pipelines),
		Name: fmt.Sprintf("Batch%d-%s", s.sectors, ssizeToName[must.One(s.spt.SectorSize())]),
		Cost: resources.Resources{
			Cpu:     1,
			Gpu:     0,
			Ram:     16 << 30,
			Storage: s.sc.StorageMulti(s.taskToSectors, storiface.FTCache|storiface.FTSealed, storiface.FTNone, ssize, storiface.PathSealing, paths.MinFreeStoragePercentage, FSOverheadSupra),
		},
		MaxFailures: 4,
		IAmBored:    passcall.Every(30*time.Second, s.schedule),
	}
}

func (s *SupraSeal) Adder(taskFunc harmonytask.AddTaskFunc) {
}

type sectorClaim struct {
	SpID         int64         `db:"sp_id"`
	SectorNumber int64         `db:"sector_number"`
	TaskIDSDR    sql.NullInt64 `db:"task_id_sdr"`
}

func (s *SupraSeal) schedule(taskFunc harmonytask.AddTaskFunc) error {
	if s.slots.Available() == 0 {
		return nil
	}

	if err := hugepageutil.CheckHugePages(36); err != nil {
		log.Warnw("huge pages check failed, try 'sudo sysctl -w vm.nr_hugepages=36' and make sure your system uses 1G huge pages", "err", err)
		return nil
	}

	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		// claim [sectors] pipeline entries

		var sectors []sectorClaim
		err := tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_sdr FROM sectors_sdr_pipeline
                                         LEFT JOIN harmony_task ht on sectors_sdr_pipeline.task_id_sdr = ht.id
                                         WHERE after_sdr = FALSE AND (task_id_sdr IS NULL OR (ht.owner_id IS NULL AND ht.name = 'SDR')) LIMIT $1`, s.sectors)
		if err != nil {
			return false, xerrors.Errorf("getting tasks: %w", err)
		}

		log.Infow("got sectors, maybe schedule", "sectors", len(sectors), "s.sectors", s.sectors)

		if len(sectors) != s.sectors {
			// not enough sectors to fill a batch, use CC scheduler
			log.Infow("not enough sectors to fill a batch, using CC scheduler", "sectors", len(sectors))
			addSectors, err := s.claimsFromCCScheduler(tx, int64(s.sectors-len(sectors)))
			if err != nil {
				return false, xerrors.Errorf("getting CC scheduler claims: %w", err)
			}
			sectors = append(sectors, addSectors...)
			log.Infow("got CC scheduler claims", "sectors", len(sectors))
		}

		if len(sectors) != s.sectors {
			return false, xerrors.Errorf("not enough sectors to fill a batch %d != %d", len(sectors), s.sectors)
		}

		// assign to pipeline entries, set task_id_sdr, task_id_tree_r, task_id_tree_c
		for _, t := range sectors {
			_, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_sdr = $1, task_id_tree_r = $1, task_id_tree_c = $1, task_id_tree_d = $1 WHERE sp_id = $2 AND sector_number = $3`, id, t.SpID, t.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating task id: %w", err)
			}

			if t.TaskIDSDR.Valid {
				// sdr task exists, remove it from the task engine
				_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, t.TaskIDSDR.Int64)
				if err != nil {
					return false, xerrors.Errorf("deleting old task: %w", err)
				}
			}
		}

		return true, nil
	})

	return nil
}

func (s *SupraSeal) claimsFromCCScheduler(tx *harmonydb.Tx, toSeal int64) ([]sectorClaim, error) {
	var enabledSchedules []struct {
		SpID         int64 `db:"sp_id"`
		ToSeal       int64 `db:"to_seal"`
		Weight       int64 `db:"weight"`
		DurationDays int64 `db:"duration_days"`
	}

	err := tx.Select(&enabledSchedules, `SELECT sp_id, to_seal, weight, duration_days FROM sectors_cc_scheduler WHERE enabled = TRUE AND weight > 0 ORDER BY weight DESC`)
	if err != nil {
		return nil, xerrors.Errorf("getting enabled schedules: %w", err)
	}

	if len(enabledSchedules) == 0 {
		return nil, nil
	}

	var totalWeight, totalToSeal int64
	for _, schedule := range enabledSchedules {
		totalWeight += schedule.Weight
		totalToSeal += schedule.ToSeal
	}

	if totalToSeal < toSeal {
		log.Debugw("not enough sectors to fill a batch from CC scheduler", "totalToSeal", totalToSeal, "toSeal", toSeal)
		return nil, nil
	}

	// Calculate proportional allocation based on weights
	var outClaims []sectorClaim
	remainingToSeal := toSeal

	for i, schedule := range enabledSchedules {
		if remainingToSeal <= 0 {
			break
		}

		// Calculate how many sectors this SP should get based on weight
		var sectorsForSP int64
		if i == len(enabledSchedules)-1 {
			// Last SP gets the remaining sectors
			sectorsForSP = remainingToSeal
		} else {
			// Proportional allocation based on weight
			sectorsForSP = (toSeal * schedule.Weight) / totalWeight
			if sectorsForSP > schedule.ToSeal {
				sectorsForSP = schedule.ToSeal
			}
			if sectorsForSP > remainingToSeal {
				sectorsForSP = remainingToSeal
			}
		}

		if sectorsForSP == 0 {
			continue
		}

		// Allocate sector numbers for this SP
		maddr, err := address.NewIDAddress(uint64(schedule.SpID))
		if err != nil {
			return nil, xerrors.Errorf("getting miner address for %d: %w", schedule.SpID, err)
		}

		sectorNumbers, err := seal.AllocateSectorNumbers(context.Background(), s.api, tx, maddr, int(sectorsForSP))
		if err != nil {
			return nil, xerrors.Errorf("allocating sector numbers for %d: %w", schedule.SpID, err)
		}

		// Create sector claims for allocated sectors
		for _, sectorNum := range sectorNumbers {
			outClaims = append(outClaims, sectorClaim{
				SpID:         schedule.SpID,
				SectorNumber: int64(sectorNum),
				TaskIDSDR:    sql.NullInt64{}, // New sector, no existing task
			})

			userDuration := int64(schedule.DurationDays) * builtin.EpochsInDay

			if miner12.MaxSectorExpirationExtension < userDuration {
				return nil, xerrors.Errorf("duration exceeds max allowed: %d > %d", userDuration, miner12.MaxSectorExpirationExtension)
			}
			if miner12.MinSectorExpiration > userDuration {
				return nil, xerrors.Errorf("duration is too short: %d < %d", userDuration, miner12.MinSectorExpiration)
			}

			// Insert new sector into sectors_sdr_pipeline
			_, err := tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof, user_sector_duration_epochs) 
				VALUES ($1, $2, $3, $4)`,
				schedule.SpID, sectorNum, s.spt, userDuration)
			if err != nil {
				return nil, xerrors.Errorf("inserting new sector %d for SP %d: %w", sectorNum, schedule.SpID, err)
			}
		}

		// Update the to_seal count for this SP
		_, err = tx.Exec(`UPDATE sectors_cc_scheduler SET to_seal = to_seal - $1 WHERE sp_id = $2`, sectorsForSP, schedule.SpID)
		if err != nil {
			return nil, xerrors.Errorf("updating to_seal for SP %d: %w", schedule.SpID, err)
		}

		remainingToSeal -= sectorsForSP
		log.Debugw("allocated sectors from CC scheduler", "sp_id", schedule.SpID, "count", sectorsForSP, "remaining", remainingToSeal, "totalWeight", totalWeight, "totalToSeal", totalToSeal)
	}

	if len(outClaims) != int(toSeal) {
		return nil, xerrors.Errorf("failed to allocate expected number of sectors: got %d, wanted %d", len(outClaims), toSeal)
	}

	return outClaims, nil
}

func (s *SupraSeal) taskToSectors(id harmonytask.TaskID) ([]ffi.SectorRef, error) {
	var sectors []ffi.SectorRef

	err := s.db.Select(context.Background(), &sectors, `SELECT sp_id, sector_number, reg_seal_proof FROM sectors_sdr_pipeline WHERE task_id_sdr = $1 AND task_id_tree_r = $1 AND task_id_tree_c = $1 AND task_id_tree_d = $1`, id)
	if err != nil {
		return nil, xerrors.Errorf("getting sector params: %w", err)
	}

	return sectors, nil
}

var FSOverheadSupra = map[storiface.SectorFileType]int{ // 10x overheads
	storiface.FTUnsealed: storiface.FSOverheadDen,
	storiface.FTSealed:   storiface.FSOverheadDen,
	storiface.FTCache:    11, // C + R' (no 11 layers + D(2x ssize));
}

func SupraSpaceUse(ft storiface.SectorFileType, ssize abi.SectorSize) (uint64, error) {
	var need uint64
	for _, pathType := range ft.AllSet() {

		oh, ok := FSOverheadSupra[pathType]
		if !ok {
			return 0, xerrors.Errorf("no seal overhead info for %s", pathType)
		}

		need += uint64(oh) * uint64(ssize) / storiface.FSOverheadDen
	}

	return need, nil
}

func init() {
	spts := []abi.RegisteredSealProof{
		abi.RegisteredSealProof_StackedDrg32GiBV1_1,
		abi.RegisteredSealProof_StackedDrg64GiBV1_1,
	}

	batchSizes := []int{1, 2, 4, 8, 16, 32, 64, 128}

	for _, spt := range spts {
		for _, batchSize := range batchSizes {
			_ = harmonytask.Reg(&SupraSeal{
				spt:     spt,
				sectors: batchSize,
			})
		}
	}

}

var _ harmonytask.TaskInterface = &SupraSeal{}
