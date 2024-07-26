// Package tasks contains tasks that can be run by the curio command.
package tasks

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/exp/maps"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/taskhelp/usertaskmgt"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/fastparamfetch"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/tasks/gc"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/metadata"
	piece2 "github.com/filecoin-project/curio/tasks/piece"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/filecoin-project/curio/tasks/snap"
	window2 "github.com/filecoin-project/curio/tasks/window"
	"github.com/filecoin-project/curio/tasks/winning"

	proofparams "github.com/filecoin-project/lotus/build/proof-params"
	"github.com/filecoin-project/lotus/lib/lazy"
	"github.com/filecoin-project/lotus/lib/result"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

var log = logging.Logger("curio/deps")

func WindowPostScheduler(ctx context.Context, fc config.CurioFees, pc config.CurioProvingConfig,
	api api.Chain, verif storiface.Verifier, paramck func() (bool, error), sender *message.Sender, chainSched *chainsched.CurioChainSched,
	as *multictladdr.MultiAddressSelector, addresses map[dtypes.MinerAddress]bool, db *harmonydb.DB,
	stor paths.Store, idx paths.SectorIndex, max int) (*window2.WdPostTask, *window2.WdPostSubmitTask, *window2.WdPostRecoverDeclareTask, error) {

	// todo config
	ft := window2.NewSimpleFaultTracker(stor, idx, pc.ParallelCheckLimit, time.Duration(pc.SingleCheckTimeout), time.Duration(pc.PartitionCheckTimeout))

	computeTask, err := window2.NewWdPostTask(db, api, ft, stor, verif, paramck, chainSched, addresses, max, pc.ParallelCheckLimit, time.Duration(pc.SingleCheckTimeout))
	if err != nil {
		return nil, nil, nil, err
	}

	submitTask, err := window2.NewWdPostSubmitTask(chainSched, sender, db, api, fc.MaxWindowPoStGasFee, as)
	if err != nil {
		return nil, nil, nil, err
	}

	recoverTask, err := window2.NewWdPostRecoverDeclareTask(sender, db, api, ft, as, chainSched, fc.MaxWindowPoStGasFee, addresses)
	if err != nil {
		return nil, nil, nil, err
	}

	return computeTask, submitTask, recoverTask, nil
}

func StartTasks(ctx context.Context, dependencies *deps.Deps) (*harmonytask.TaskEngine, error) {
	cfg := dependencies.Cfg
	db := dependencies.DB
	full := dependencies.Chain
	verif := dependencies.Verif
	as := dependencies.As
	maddrs := dependencies.Maddrs
	stor := dependencies.Stor
	lstor := dependencies.LocalStore
	si := dependencies.Si
	bstore := dependencies.Bstore
	var activeTasks []harmonytask.TaskInterface

	sender, sendTask := message.NewSender(full, full, db)
	activeTasks = append(activeTasks, sendTask)

	chainSched := chainsched.New(full)

	// paramfetch
	var fetchOnce sync.Once
	var fetchResult atomic.Pointer[result.Result[bool]]

	var asyncParams = func() func() (bool, error) {
		fetchOnce.Do(func() {
			go func() {
				for spt := range dependencies.ProofTypes {

					provingSize := uint64(must.One(spt.SectorSize()))
					err := fastparamfetch.GetParams(context.TODO(), proofparams.ParametersJSON(), proofparams.SrsJSON(), provingSize)

					if err != nil {
						log.Errorw("failed to fetch params", "error", err)
						fetchResult.Store(&result.Result[bool]{Value: false, Error: err})
						return
					}
				}

				fetchResult.Store(&result.Result[bool]{Value: true})
			}()
		})

		return func() (bool, error) {
			res := fetchResult.Load()
			if res == nil {
				return false, nil
			}
			return res.Value, res.Error
		}
	}

	///////////////////////////////////////////////////////////////////////
	///// Task Selection
	///////////////////////////////////////////////////////////////////////
	{
		// PoSt

		if cfg.Subsystems.EnableWindowPost {
			wdPostTask, wdPoStSubmitTask, derlareRecoverTask, err := WindowPostScheduler(
				ctx, cfg.Fees, cfg.Proving, full, verif, asyncParams(), sender, chainSched,
				as, maddrs, db, stor, si, cfg.Subsystems.WindowPostMaxTasks)

			if err != nil {
				return nil, err
			}
			activeTasks = append(activeTasks, wdPostTask, wdPoStSubmitTask, derlareRecoverTask)
		}

		if cfg.Subsystems.EnableWinningPost {
			store := dependencies.Stor
			winPoStTask := winning.NewWinPostTask(cfg.Subsystems.WinningPostMaxTasks, db, store, verif, asyncParams(), full, maddrs)
			inclCkTask := winning.NewInclusionCheckTask(db, full)
			activeTasks = append(activeTasks, winPoStTask, inclCkTask)
		}
	}

	slrLazy := lazy.MakeLazy(func() (*ffi.SealCalls, error) {
		return ffi.NewSealCalls(stor, lstor, si), nil
	})

	{
		// Piece handling
		if cfg.Subsystems.EnableParkPiece {
			parkPieceTask, err := piece2.NewParkPieceTask(db, must.One(slrLazy.Val()), cfg.Subsystems.ParkPieceMaxTasks)
			if err != nil {
				return nil, err
			}
			cleanupPieceTask := piece2.NewCleanupPieceTask(db, must.One(slrLazy.Val()), 0)
			activeTasks = append(activeTasks, parkPieceTask, cleanupPieceTask)
		}
	}

	hasAnySealingTask := cfg.Subsystems.EnableSealSDR ||
		cfg.Subsystems.EnableSealSDRTrees ||
		cfg.Subsystems.EnableSendPrecommitMsg ||
		cfg.Subsystems.EnablePoRepProof ||
		cfg.Subsystems.EnableMoveStorage ||
		cfg.Subsystems.EnableSendCommitMsg ||
		cfg.Subsystems.EnableUpdateEncode ||
		cfg.Subsystems.EnableUpdateProve ||
		cfg.Subsystems.EnableUpdateSubmit
	if hasAnySealingTask {
		sealingTasks, err := addSealingTasks(ctx, hasAnySealingTask, db, full, sender, as, cfg, slrLazy, asyncParams, si, stor, bstore)
		if err != nil {
			return nil, err
		}
		activeTasks = append(activeTasks, sealingTasks...)
	}

	amTask := alertmanager.NewAlertTask(full, db, cfg.Alerting, dependencies.Al)
	activeTasks = append(activeTasks, amTask)

	minerAddresses := make([]string, 0, len(maddrs))
	for k := range maddrs {
		minerAddresses = append(minerAddresses, address.Address(k).String())
	}

	log.Infow("This Curio instance handles",
		"miner_addresses", minerAddresses,
		"tasks", lo.Map(activeTasks, func(t harmonytask.TaskInterface, _ int) string { return t.TypeDetails().Name }))

	// harmony treats the first task as highest priority, so reverse the order
	// (we could have just appended to this list in the reverse order, but defining
	//  tasks in pipeline order is more intuitive)

	usertaskmgt.WrapTasks(activeTasks, dependencies.Cfg.Subsystems.UserScheduler, dependencies.DB, dependencies.ListenAddr)
	ht, err := harmonytask.New(db, activeTasks, dependencies.ListenAddr)
	if err != nil {
		return nil, err
	}
	go machineDetails(dependencies, activeTasks, ht.ResourcesAvailable().MachineID, dependencies.Name)

	if hasAnySealingTask {
		watcher, err := message.NewMessageWatcher(db, ht, chainSched, full)
		if err != nil {
			return nil, err
		}
		_ = watcher
	}

	if cfg.Subsystems.EnableWindowPost || hasAnySealingTask {
		go chainSched.Run(ctx)
	}

	return ht, nil
}

func addSealingTasks(
	ctx context.Context, hasAnySealingTask bool, db *harmonydb.DB, full api.Chain, sender *message.Sender,
	as *multictladdr.MultiAddressSelector, cfg *config.CurioConfig, slrLazy *lazy.Lazy[*ffi.SealCalls],
	asyncParams func() func() (bool, error), si paths.SectorIndex, stor *paths.Remote,
	bstore curiochain.CurioBlockstore) ([]harmonytask.TaskInterface, error) {
	var activeTasks []harmonytask.TaskInterface
	// Sealing / Snap

	var sp *seal.SealPoller
	var slr *ffi.SealCalls
	if hasAnySealingTask {
		sp = seal.NewPoller(db, full)
		go sp.RunPoller(ctx)

		slr = must.One(slrLazy.Val())
	}

	// NOTE: Tasks with the LEAST priority are at the top
	if cfg.Subsystems.EnableSealSDR {
		sdrTask := seal.NewSDRTask(full, db, sp, slr, cfg.Subsystems.SealSDRMaxTasks)
		activeTasks = append(activeTasks, sdrTask)
	}
	if cfg.Subsystems.EnableSealSDRTrees {
		treeDTask := seal.NewTreeDTask(sp, db, slr, cfg.Subsystems.SealSDRTreesMaxTasks)
		treeRCTask := seal.NewTreeRCTask(sp, db, slr, cfg.Subsystems.SealSDRTreesMaxTasks)
		finalizeTask := seal.NewFinalizeTask(cfg.Subsystems.FinalizeMaxTasks, sp, slr, db)
		activeTasks = append(activeTasks, treeDTask, treeRCTask, finalizeTask)
	}
	if cfg.Subsystems.EnableSendPrecommitMsg {
		precommitTask := seal.NewSubmitPrecommitTask(sp, db, full, sender, as, cfg.Fees.MaxPreCommitGasFee, cfg.Fees.CollateralFromMinerBalance, cfg.Fees.DisableCollateralFallback)
		activeTasks = append(activeTasks, precommitTask)
	}
	if cfg.Subsystems.EnablePoRepProof {
		porepTask := seal.NewPoRepTask(db, full, sp, slr, asyncParams(), cfg.Subsystems.PoRepProofMaxTasks)
		activeTasks = append(activeTasks, porepTask)
	}
	if cfg.Subsystems.EnableMoveStorage {
		moveStorageTask := seal.NewMoveStorageTask(sp, slr, db, cfg.Subsystems.MoveStorageMaxTasks)
		moveStorageSnapTask := snap.NewMoveStorageTask(slr, db, cfg.Subsystems.MoveStorageMaxTasks)
		activeTasks = append(activeTasks, moveStorageTask, moveStorageSnapTask)
	}
	if cfg.Subsystems.EnableSendCommitMsg {
		commitTask := seal.NewSubmitCommitTask(sp, db, full, sender, as, cfg)
		activeTasks = append(activeTasks, commitTask)
	}

	if cfg.Subsystems.EnableUpdateEncode {
		encodeTask := snap.NewEncodeTask(slr, db, cfg.Subsystems.UpdateEncodeMaxTasks)
		activeTasks = append(activeTasks, encodeTask)
	}
	if cfg.Subsystems.EnableUpdateProve {
		proveTask := snap.NewProveTask(slr, db, asyncParams(), cfg.Subsystems.UpdateProveMaxTasks)
		activeTasks = append(activeTasks, proveTask)
	}
	if cfg.Subsystems.EnableUpdateSubmit {
		submitTask := snap.NewSubmitTask(db, full, bstore, sender, as, cfg)
		activeTasks = append(activeTasks, submitTask)
	}
	activeTasks = lo.Reverse(activeTasks)

	if hasAnySealingTask {
		// Sealing nodes maintain storage index when bored
		storageEndpointGcTask := gc.NewStorageEndpointGC(si, stor, db)
		pipelineGcTask := gc.NewPipelineGC(db)
		storageGcMarkTask := gc.NewStorageGCMark(si, stor, db, bstore, full)
		storageGcSweepTask := gc.NewStorageGCSweep(db, stor, si)

		sectorMetadataTask := metadata.NewSectorMetadataTask(db, bstore, full)

		activeTasks = append(activeTasks, storageEndpointGcTask, pipelineGcTask, storageGcMarkTask, storageGcSweepTask, sectorMetadataTask)
	}

	return activeTasks, nil
}

func machineDetails(deps *deps.Deps, activeTasks []harmonytask.TaskInterface, machineID int, machineName string) {
	taskNames := lo.Map(activeTasks, func(item harmonytask.TaskInterface, _ int) string {
		return item.TypeDetails().Name
	})

	miners := lo.Map(maps.Keys(deps.Maddrs), func(item dtypes.MinerAddress, _ int) string {
		return address.Address(item).String()
	})
	sort.Strings(miners)

	_, err := deps.DB.Exec(context.Background(), `INSERT INTO harmony_machine_details 
		(tasks, layers, startup_time, miners, machine_id, machine_name) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (machine_id) DO UPDATE SET tasks=$1, layers=$2, startup_time=$3, miners=$4, machine_id=$5, machine_name=$6`,
		strings.Join(taskNames, ","), strings.Join(deps.Layers, ","),
		time.Now(), strings.Join(miners, ","), machineID, machineName)

	if err != nil {
		log.Errorf("failed to update machine details: %s", err)
		return
	}

	// maybePostWarning
	if !lo.Contains(taskNames, "WdPost") && !lo.Contains(taskNames, "WinPost") {
		// Maybe we aren't running a PoSt for these miners?
		var allMachines []struct {
			MachineID int    `db:"machine_id"`
			Miners    string `db:"miners"`
			Tasks     string `db:"tasks"`
		}
		err := deps.DB.Select(context.Background(), &allMachines, `SELECT machine_id, miners, tasks FROM harmony_machine_details`)
		if err != nil {
			log.Errorf("failed to get machine details: %s", err)
			return
		}

		for _, miner := range miners {
			var myPostIsHandled bool
			for _, m := range allMachines {
				if !lo.Contains(strings.Split(m.Miners, ","), miner) {
					continue
				}
				if lo.Contains(strings.Split(m.Tasks, ","), "WdPost") && lo.Contains(strings.Split(m.Tasks, ","), "WinPost") {
					myPostIsHandled = true
					break
				}
			}
			if !myPostIsHandled {
				log.Errorf("No PoSt tasks are running for miner %s. Start handling PoSts immediately with:\n\tcurio run --layers=\"post\" ", miner)
			}
		}
	}
}
