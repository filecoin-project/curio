// Package tasks contains tasks that can be run by the curio command.
package tasks

import (
	"context"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/exp/maps"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/fastparamfetch"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/slotmgr"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/libp2p"
	"github.com/filecoin-project/curio/tasks/balancemgr"
	"github.com/filecoin-project/curio/tasks/f3"
	"github.com/filecoin-project/curio/tasks/gc"
	"github.com/filecoin-project/curio/tasks/indexing"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/metadata"
	"github.com/filecoin-project/curio/tasks/pay"
	"github.com/filecoin-project/curio/tasks/pdp"
	piece2 "github.com/filecoin-project/curio/tasks/piece"
	"github.com/filecoin-project/curio/tasks/proofshare"
	"github.com/filecoin-project/curio/tasks/scrub"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/filecoin-project/curio/tasks/sealsupra"
	"github.com/filecoin-project/curio/tasks/snap"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
	"github.com/filecoin-project/curio/tasks/unseal"
	window2 "github.com/filecoin-project/curio/tasks/window"
	"github.com/filecoin-project/curio/tasks/winning"

	proofparams "github.com/filecoin-project/lotus/build/proof-params"
	"github.com/filecoin-project/lotus/lib/lazy"
	"github.com/filecoin-project/lotus/lib/result"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

var log = logging.Logger("curio/deps")

func WindowPostScheduler(ctx context.Context, fc config.CurioFees, pc config.CurioProvingConfig,
	api api.Chain, verif storiface.Verifier, paramck func() (bool, error), sender *message.Sender, chainSched *chainsched.CurioChainSched,
	as *multictladdr.MultiAddressSelector, addresses map[dtypes.MinerAddress]bool, db *harmonydb.DB,
	stor paths.Store, idx paths.SectorIndex, max int,
) (*window2.WdPostTask, *window2.WdPostSubmitTask, *window2.WdPostRecoverDeclareTask, error) {
	// todo config
	ft := window2.NewSimpleFaultTracker(stor, idx, pc.ParallelCheckLimit, pc.SingleCheckTimeout, pc.PartitionCheckTimeout)

	computeTask, err := window2.NewWdPostTask(db, api, ft, stor, verif, paramck, chainSched, addresses, max, pc.ParallelCheckLimit, pc.SingleCheckTimeout)
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

func StartTasks(ctx context.Context, dependencies *deps.Deps, shutdownChan chan struct{}) (*harmonytask.TaskEngine, error) {
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
	machine := dependencies.ListenAddr
	prover := dependencies.Prover
	iStore := dependencies.IndexStore
	pp := dependencies.SectorReader

	chainSched := chainsched.New(full)

	var activeTasks []harmonytask.TaskInterface

	sender, sendTask := message.NewSender(full, full, db, cfg.Fees.MaximizeFeeCap)
	balanceMgrTask := balancemgr.NewBalanceMgrTask(db, full, chainSched, sender)
	activeTasks = append(activeTasks, sendTask, balanceMgrTask)
	dependencies.Sender = sender

	// paramfetch
	var fetchOnce sync.Once
	var fetchResult atomic.Pointer[result.Result[bool]]

	asyncParams := func() func() (bool, error) {
		fetchOnce.Do(func() {
			go func() {
				seenSizes := make(map[uint64]bool)

				for spt := range dependencies.ProofTypes {
					provingSize := uint64(must.One(spt.SectorSize()))
					if seenSizes[provingSize] {
						continue
					}
					seenSizes[provingSize] = true

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

	// eth message sender as needed
	var senderEth *message.SenderETH
	var senderEthOnce sync.Once
	getSenderEth := func() *message.SenderETH {
		senderEthOnce.Do(func() {
			ec, err := dependencies.EthClient.Val()
			if err != nil {
				log.Errorw("failed to get eth client", "error", err)
				return
			}

			var ethSenderTask *message.SendTaskETH
			senderEth, ethSenderTask = message.NewSenderETH(ec, db)
			activeTasks = append(activeTasks, ethSenderTask)
		})
		return senderEth
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

			if os.Getenv("CURIO_DISABLE_F3") != "1" {
				f3Task := f3.NewF3Task(db, full, maddrs)
				activeTasks = append(activeTasks, f3Task)
			}

			// Warn if also running a sealing task
			if cfg.Subsystems.EnableSealSDR || cfg.Subsystems.EnableSealSDRTrees || cfg.Subsystems.EnableSendPrecommitMsg || cfg.Subsystems.EnablePoRepProof || cfg.Subsystems.EnableMoveStorage || cfg.Subsystems.EnableSendCommitMsg || cfg.Subsystems.EnableUpdateEncode || cfg.Subsystems.EnableUpdateProve || cfg.Subsystems.EnableUpdateSubmit {
				log.Error("It's unsafe to run PoSt and sealing tasks concurrently.")
				dependencies.Alert.AddAlert("It's unsafe to run PoSt and sealing tasks concurrently.")
			}
		}
	}

	slrLazy := lazy.MakeLazy(func() (*ffi.SealCalls, error) {
		return ffi.NewSealCalls(stor, lstor, si), nil
	})

	hasAnySealingTask := cfg.Subsystems.EnableSealSDR ||
		cfg.Subsystems.EnableSealSDRTrees ||
		cfg.Subsystems.EnableSendPrecommitMsg ||
		cfg.Subsystems.EnablePoRepProof ||
		cfg.Subsystems.EnableMoveStorage ||
		cfg.Subsystems.EnableSendCommitMsg ||
		cfg.Subsystems.EnableBatchSeal ||
		cfg.Subsystems.EnableUpdateEncode ||
		cfg.Subsystems.EnableUpdateProve ||
		cfg.Subsystems.EnableUpdateSubmit ||
		cfg.Subsystems.EnableCommP ||
		cfg.Subsystems.EnableProofShare ||
		cfg.Subsystems.EnableRemoteProofs

	if hasAnySealingTask {
		sealingTasks, err := addSealingTasks(ctx, hasAnySealingTask, db, full, sender, as, cfg, slrLazy, asyncParams, si, stor, bstore, machine, prover)
		if err != nil {
			return nil, err
		}
		activeTasks = append(activeTasks, sealingTasks...)
	}

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

	miners := make([]address.Address, 0, len(maddrs))
	for k := range maddrs {
		miners = append(miners, address.Address(k))
	}

	if cfg.Subsystems.EnableBalanceManager {
		balMgrTask, err := storage_market.NewBalanceManager(full, miners, cfg, sender)
		if err != nil {
			return nil, err
		}
		activeTasks = append(activeTasks, balMgrTask)
	}

	{
		// Market tasks
		var dm *storage_market.CurioStorageDealMarket
		if cfg.Subsystems.EnableDealMarket {
			// Main market poller should run on all nodes
			dm = storage_market.NewCurioStorageDealMarket(miners, db, cfg, si, full, as)
			err := dm.StartMarket(ctx)
			if err != nil {
				return nil, err
			}

			if cfg.Subsystems.EnableCommP {
				commpTask := storage_market.NewCommpTask(dm, db, must.One(slrLazy.Val()), full, cfg.Subsystems.CommPMaxTasks)
				activeTasks = append(activeTasks, commpTask)
			}

			// PSD and Deal find task do not require many resources. They can run on all machines
			psdTask := storage_market.NewPSDTask(dm, db, sender, as, &cfg.Market.StorageMarketConfig.MK12, full)
			dealFindTask := storage_market.NewFindDealTask(dm, db, full, &cfg.Market.StorageMarketConfig.MK12)

			checkIndexesTask := indexing.NewCheckIndexesTask(db, iStore)

			activeTasks = append(activeTasks, psdTask, dealFindTask, checkIndexesTask)

			// Start libp2p hosts and handle streams. This is a special function which calls the shutdown channel
			// instead of returning the error. This design is to allow libp2p take over if required
			go libp2p.NewDealProvider(ctx, db, cfg, dm.MK12Handler, full, sender, miners, machine, shutdownChan)
		}
		sc, err := slrLazy.Val()
		if err != nil {
			return nil, err
		}
		var sdeps cuhttp.ServiceDeps
		idxMax := taskhelp.Max(cfg.Subsystems.IndexingMaxTasks)

		if cfg.Subsystems.EnablePDP {
			es := getSenderEth()
			sdeps.EthSender = es

			pdp.NewDataSetWatch(db, must.One(dependencies.EthClient.Val()), chainSched)

			pay.NewSettleWatcher(db, must.One(dependencies.EthClient.Val()), chainSched)
			pdp.NewDataSetDeleteWatcher(db, must.One(dependencies.EthClient.Val()), chainSched)
			pdp.NewTerminateServiceWatcher(db, must.One(dependencies.EthClient.Val()), chainSched)
			pdp.NewPieceDeleteWatcher(&cfg.HTTP, db, must.One(dependencies.EthClient.Val()), chainSched, iStore)

			pdpProveTask := pdp.NewProveTask(chainSched, db, must.One(dependencies.EthClient.Val()), dependencies.Chain, es, dependencies.CachedPieceReader, iStore)
			pdpNextProvingPeriodTask := pdp.NewNextProvingPeriodTask(db, must.One(dependencies.EthClient.Val()), dependencies.Chain, chainSched, es)
			pdpInitProvingPeriodTask := pdp.NewInitProvingPeriodTask(db, must.One(dependencies.EthClient.Val()), dependencies.Chain, chainSched, es)
			pdpNotifTask := pdp.NewPDPNotifyTask(ctx, db)
			pdpPullPieceTask := pdp.NewPDPPullPieceTask(ctx, db, lstor, 4)
			pdpIndexingTask := indexing.NewPDPIndexingTask(db, iStore, dependencies.CachedPieceReader, cfg, idxMax)
			pdpIpniTask := indexing.NewPDPIPNITask(db, sc, dependencies.CachedPieceReader, cfg, idxMax)
			pdpTerminate := pdp.NewTerminateServiceTask(db, must.One(dependencies.EthClient.Val()), senderEth)
			pdpDelete := pdp.NewDeleteDataSetTask(db, must.One(dependencies.EthClient.Val()), senderEth)
			payTask := pay.NewSettleTask(db, must.One(dependencies.EthClient.Val()), senderEth)
			pdpSaveCacheTask := pdp.NewTaskPDPSaveCache(db, dependencies.CachedPieceReader, iStore)
			activeTasks = append(activeTasks, pdpProveTask, pdpNotifTask, pdpPullPieceTask, pdpNextProvingPeriodTask, pdpInitProvingPeriodTask, pdpIndexingTask, pdpIpniTask, pdpTerminate, pdpDelete, payTask, pdpSaveCacheTask)
		}

		indexingTask := indexing.NewIndexingTask(db, sc, iStore, pp, cfg, idxMax)
		ipniTask := indexing.NewIPNITask(db, sc, iStore, pp, cfg, idxMax)
		activeTasks = append(activeTasks, ipniTask, indexingTask)

		if cfg.HTTP.Enable {
			err = cuhttp.StartHTTPServer(ctx, dependencies, &sdeps, dm)
			if err != nil {
				return nil, xerrors.Errorf("failed to start the HTTP server: %w", err)
			}
		}
	}

	amTask := alertmanager.NewAlertTask(full, db, cfg.Alerting, dependencies.Al)
	activeTasks = append(activeTasks, amTask)

	log.Infow("This Curio instance handles",
		"miner_addresses", miners,
		"tasks", lo.Map(activeTasks, func(t harmonytask.TaskInterface, _ int) string { return t.TypeDetails().Name }))

	ht, err := harmonytask.New(db, activeTasks, dependencies.ListenAddr)
	if err != nil {
		return nil, err
	}
	go machineDetails(dependencies, activeTasks, ht.ResourcesAvailable().MachineID, dependencies.Name)

	*dependencies.MachineID = int64(ht.ResourcesAvailable().MachineID)

	if hasAnySealingTask {
		watcher, err := message.NewMessageWatcher(db, ht, chainSched, full)
		if err != nil {
			return nil, err
		}
		_ = watcher
	}

	if senderEth != nil {
		watcherEth, err := message.NewMessageWatcherEth(db, ht, chainSched, must.One(dependencies.EthClient.Val()))
		if err != nil {
			return nil, err
		}
		_ = watcherEth

	}

	if cfg.Subsystems.EnableWindowPost || hasAnySealingTask || senderEth != nil {
		go chainSched.Run(ctx)
	}

	return ht, nil
}

func addSealingTasks(
	ctx context.Context, hasAnySealingTask bool, db *harmonydb.DB, full api.Chain, sender *message.Sender,
	as *multictladdr.MultiAddressSelector, cfg *config.CurioConfig, slrLazy *lazy.Lazy[*ffi.SealCalls],
	asyncParams func() func() (bool, error), si paths.SectorIndex, stor *paths.Remote,
	bstore curiochain.CurioBlockstore, machineHostPort string, prover storiface.Prover,
) ([]harmonytask.TaskInterface, error) {
	var activeTasks []harmonytask.TaskInterface
	// Sealing / Snap

	var sp *seal.SealPoller
	var slr *ffi.SealCalls
	if hasAnySealingTask {
		sp = seal.NewPoller(db, full, cfg)
		go sp.RunPoller(ctx)

		slr = must.One(slrLazy.Val())
	}

	var slotMgr *slotmgr.SlotMgr
	var addFinalize bool

	// NOTE: Tasks with the LEAST priority are at the top
	if cfg.Subsystems.EnableCommP {
		scrubUnsealedTask := scrub.NewCommDCheckTask(db, slr)
		activeTasks = append(activeTasks, scrubUnsealedTask)
	}

	if cfg.Subsystems.EnableBatchSeal {
		batchSealTask, sm, err := sealsupra.NewSupraSeal(
			cfg.Seal.BatchSealSectorSize,
			cfg.Seal.BatchSealBatchSize,
			cfg.Seal.BatchSealPipelines,
			!cfg.Seal.SingleHasherPerThread,
			cfg.Seal.LayerNVMEDevices,
			machineHostPort, db, full, stor, si, slr)
		if err != nil {
			return nil, xerrors.Errorf("setting up batch sealer: %w", err)
		}
		slotMgr = sm
		activeTasks = append(activeTasks, batchSealTask)
		addFinalize = true
	}

	if cfg.Subsystems.EnableSealSDR {
		sdrMax := taskhelp.Max(cfg.Subsystems.SealSDRMaxTasks)

		sdrTask := seal.NewSDRTask(full, db, sp, slr, sdrMax, cfg.Subsystems.SealSDRMinTasks)
		keyTask := unseal.NewTaskUnsealSDR(slr, db, sdrMax, full)

		activeTasks = append(activeTasks, sdrTask, keyTask)
	}
	if cfg.Subsystems.EnableSealSDRTrees {
		treeDTask := seal.NewTreeDTask(sp, db, slr, cfg.Subsystems.SealSDRTreesMaxTasks, cfg.Subsystems.BindSDRTreeToNode)
		treeRCTask := seal.NewTreeRCTask(sp, db, slr, cfg.Subsystems.SealSDRTreesMaxTasks)
		synthTask := seal.NewSyntheticProofTask(sp, db, slr, cfg.Subsystems.SyntheticPoRepMaxTasks)
		activeTasks = append(activeTasks, treeDTask, synthTask, treeRCTask)
		addFinalize = true
	}
	if addFinalize {
		finalizeTask := seal.NewFinalizeTask(cfg.Subsystems.FinalizeMaxTasks, sp, slr, db, slotMgr)
		activeTasks = append(activeTasks, finalizeTask)
	}

	if cfg.Subsystems.EnableSendPrecommitMsg {
		precommitTask := seal.NewSubmitPrecommitTask(sp, db, full, sender, as, cfg)
		activeTasks = append(activeTasks, precommitTask)
	}
	if cfg.Subsystems.EnablePoRepProof || cfg.Subsystems.EnableRemoteProofs {
		porepTask := seal.NewPoRepTask(db, full, sp, slr, asyncParams(), cfg.Subsystems.EnablePoRepProof, cfg.Subsystems.PoRepProofMaxTasks)
		activeTasks = append(activeTasks, porepTask)
	}
	if cfg.Subsystems.EnableMoveStorage {
		moveStorageTask := seal.NewMoveStorageTask(sp, slr, db, cfg.Subsystems.MoveStorageMaxTasks)
		moveStorageSnapTask := snap.NewMoveStorageTask(slr, db, cfg.Subsystems.MoveStorageMaxTasks)

		storePieceTask, err := piece2.NewStorePieceTask(db, must.One(slrLazy.Val()), stor, cfg.Subsystems.MoveStorageMaxTasks)
		if err != nil {
			return nil, err
		}

		activeTasks = append(activeTasks, moveStorageTask, moveStorageSnapTask, storePieceTask)
		if !cfg.Subsystems.EnableParkPiece {
			// add cleanup if it's not added above with park piece
			cleanupPieceTask := piece2.NewCleanupPieceTask(db, must.One(slrLazy.Val()), 0)
			activeTasks = append(activeTasks, cleanupPieceTask)
		}

		if !cfg.Subsystems.NoUnsealedDecode {
			unsealTask := unseal.NewTaskUnsealDecode(slr, db, cfg.Subsystems.MoveStorageMaxTasks, full)
			activeTasks = append(activeTasks, unsealTask)
		}
	}
	if cfg.Subsystems.EnableSendCommitMsg {
		commitTask := seal.NewSubmitCommitTask(sp, db, full, sender, as, cfg, prover)
		activeTasks = append(activeTasks, commitTask)
	}

	if cfg.Subsystems.EnableUpdateEncode {
		encodeTask := snap.NewEncodeTask(slr, db, cfg.Subsystems.UpdateEncodeMaxTasks)
		activeTasks = append(activeTasks, encodeTask)
	}
	if cfg.Subsystems.EnableUpdateProve || cfg.Subsystems.EnableRemoteProofs {
		proveTask := snap.NewProveTask(slr, db, asyncParams(), cfg.Subsystems.EnableRemoteProofs, cfg.Subsystems.UpdateProveMaxTasks)
		activeTasks = append(activeTasks, proveTask)
	}
	if cfg.Subsystems.EnableUpdateSubmit {
		submitTask := snap.NewSubmitTask(db, full, bstore, sender, as, cfg)
		activeTasks = append(activeTasks, submitTask)
	}

	if cfg.Subsystems.EnableProofShare {
		requestProofsTask := proofshare.NewTaskRequestProofs(db, full, asyncParams())
		provideSnarkTask := proofshare.NewTaskProvideSnark(db, asyncParams(), cfg.Subsystems.ProofShareMaxTasks)
		submitTask := proofshare.NewTaskSubmit(db, full)
		autosettleTask := proofshare.NewTaskAutosettle(db, full, sender)
		activeTasks = append(activeTasks, requestProofsTask, provideSnarkTask, submitTask, autosettleTask)
	}

	if cfg.Subsystems.EnableRemoteProofs {
		router := common.NewServiceCustomSend(full, nil)
		remoteUploadTask := proofshare.NewTaskClientUpload(db, full, stor, router, cfg.Subsystems.RemoteProofMaxUploads)
		remotePollTask := proofshare.NewTaskClientPoll(db, full)
		remoteSendTask := proofshare.NewTaskClientSend(db, full, router)
		activeTasks = append(activeTasks, remoteUploadTask, remotePollTask, remoteSendTask)
	}

	// harmony treats the first task as highest priority, so reverse the order
	// (we could have just appended to this list in the reverse order, but defining
	//  tasks in pipeline order is more intuitive)
	slices.Reverse(activeTasks)

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
