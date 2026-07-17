package pdpnode

import (
	"context"

	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources/ffigpu"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/tasks/gc"
	"github.com/filecoin-project/curio/tasks/indexing"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/pay"
	"github.com/filecoin-project/curio/tasks/pdp"
	"github.com/filecoin-project/curio/tasks/pdpv0"
)

// TaskResult is the output of RegisterTasks.
type TaskResult struct {
	Engine      *harmonytask.TaskEngine
	AlertTask   *alertmanager.AlertTask
	ServiceDeps servicedeps.Deps
	EthSender   *message.SenderETH
}

type pdpTaskBundle struct {
	tasks     []harmonytask.TaskInterface
	amTask    *alertmanager.AlertTask
	ethSender *message.SenderETH
}

func buildPDPTasks(ctx context.Context, d *Deps, chainSched *chainsched.CurioChainSched, pdponly bool) (*pdpTaskBundle, error) {
	cfg := d.Cfg
	db := d.DB

	var tasks []harmonytask.TaskInterface

	if pdponly {
		_, sendTask := message.NewSender(d.Chain, d.Chain, db, cfg.Fees.MaximizeFeeCap)
		tasks = append(tasks, sendTask)
	}

	// Obtain eth client once; PDP is entirely non-functional without it.
	ethClient, err := d.EthClient.Val()
	if err != nil {
		return nil, xerrors.Errorf("eth client required for PDP: %w", err)
	}

	senderEth, ethSenderTask := message.NewSenderETH(ethClient, db)
	tasks = append(tasks, ethSenderTask, message.NewMessageWaitsEthGCTask(db, ethClient))

	pdp.NewWatcherDataSetCreate(db, ethClient, chainSched)
	pdp.NewWatcherPieceAdd(db, chainSched, ethClient)
	pdp.NewWatcherDelete(db, chainSched)
	pdp.NewWatcherPieceDelete(db, chainSched)

	tasks = append(tasks,
		pdp.NewPDPNotifyTask(db),
		pdp.NewProveTask(chainSched, db, ethClient, d.Chain, senderEth, d.CachedPieceReader, d.IndexStore),
		pdp.NewNextProvingPeriodTask(db, ethClient, d.Chain, chainSched, senderEth),
		pdp.NewInitProvingPeriodTask(db, ethClient, d.Chain, chainSched, senderEth),
		pdp.NewPDPCommpTask(db, d.PieceIO, cfg.Subsystems.CommPMaxTasks),
		pdp.NewPDPTaskAddPiece(db, senderEth, ethClient),
		pdp.NewPDPTaskAddDataSet(db, senderEth, ethClient, d.Chain),
		pdp.NewAggregatePDPDealTask(db, d.PieceIO),
		pdp.NewTaskPDPSaveCache(db, d.CachedPieceReader, d.IndexStore),
		pdp.NewPDPTaskDeletePiece(db, senderEth, ethClient),
		pdp.NewPDPTaskDeleteDataSet(db, senderEth, ethClient, d.Chain),
	)

	w := pdpv0.NewPDPv0Watcher(db, ethClient, chainSched, d.Al)
	pdpv0.NewDataSetWatch(w)
	pay.NewSettleWatcher(w)
	pdpv0.NewDataSetDeleteWatcher(w)
	pdpv0.NewCleanupPiecesWatcher(w)
	pdpv0.NewTerminateServiceWatcher(w)

	tasks = append(tasks,
		pdpv0.NewProveTask(db, ethClient, d.Chain, w, senderEth, d.CachedPieceReader, d.IndexStore),
		pdpv0.NewNextProvingPeriodTask(db, ethClient, d.Chain, w, senderEth),
		pdpv0.NewInitProvingPeriodTask(db, ethClient, d.Chain, w, senderEth),
		pdpv0.NewPDPNotifyTask(ctx, db),
		pdpv0.NewPDPPullPieceTask(ctx, db, d.PieceIO, cfg.Subsystems.PDPPullPieceMaxTasks),
		pdpv0.NewTerminateServiceTask(db, ethClient, senderEth),
		pdpv0.NewDeleteDataSetTask(db, ethClient, senderEth),
		pdpv0.NewCleanupPiecesTask(db, ethClient, senderEth),
		pdpv0.NewTaskChainSync(db, ethClient, senderEth),
		pay.NewSettleTask(db, ethClient, senderEth, d.Al), // Move this to a common section once PDP v1 is live
		pdpv0.NewTaskPDPSaveCache(db, d.CachedPieceReader, d.IndexStore),
		pdpv0.NewPieceGCTask(&cfg.HTTP, db, d.IndexStore),
		pdpv0.NewReorgCheckTask(db, ethClient, d.Chain),
	)

	// Start PDP watcher after all internal watcher are created
	w.Run(ctx)

	idxMax := taskhelp.Max(cfg.Subsystems.IndexingMaxTasks)
	tasks = append(tasks,
		indexing.NewPDPIndexingTask(db, d.IndexStore, d.CachedPieceReader, cfg, idxMax),
		indexing.NewPDPIPNITask(db, cfg, idxMax, d.IndexStore),
		indexing.NewPDPV0IndexingTask(db, d.IndexStore, d.CachedPieceReader, cfg, idxMax),
		indexing.NewPDPV0IPNITask(db, cfg, idxMax, d.IndexStore),
	)

	if pdponly {
		amTask := alertmanager.NewAlertTask(d.Chain, db, cfg.Alerting)
		tasks = append(tasks, amTask, gc.NewPieceCleanupTask(db, d.IndexStore))
		return &pdpTaskBundle{
			tasks:     tasks,
			amTask:    amTask,
			ethSender: senderEth,
		}, nil
	}

	return &pdpTaskBundle{
		tasks:     tasks,
		amTask:    nil,
		ethSender: senderEth,
	}, nil
}

// RegisterTasks wires PDP harmony tasks and returns the task engine.
func RegisterTasks(ctx context.Context, d *Deps) (*TaskResult, error) {
	chainSched := chainsched.New(d.Chain)

	bundle, err := buildPDPTasks(ctx, d, chainSched, true)
	if err != nil {
		return nil, err
	}

	ht, err := harmonytask.New(d.DB, bundle.tasks, d.MachineHost, ffigpu.Inspector{})
	if err != nil {
		return nil, err
	}

	d.MachineID = int64(ht.ResourcesAvailable().MachineID)

	ethClient := must.One(d.EthClient.Val())

	// NewMessageWatcherEth registers itself with ht for side effects; no
	// external handle is needed after construction.
	watcherEth, err := message.NewMessageWatcherEth(d.DB, ht, chainSched, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("eth message watcher: %w", err)
	}
	_ = watcherEth

	err = message.NewMessageReplacer(ctx, message.ReplacerConfig{
		DB:         d.DB,
		ChainSched: chainSched,
		Eth: &message.EthReplacerConfig{
			Client: ethClient,
		},
	})
	if err != nil {
		return nil, xerrors.Errorf("message replacer: %w", err)
	}

	go chainSched.Run(ctx)

	return &TaskResult{
		Engine:    ht,
		AlertTask: bundle.amTask,
		EthSender: bundle.ethSender,
		ServiceDeps: servicedeps.Deps{
			EthSender: bundle.ethSender,
			AlertTask: bundle.amTask,
		},
	}, nil
}

// AppendTasks adds PDP tasks to a curio task list.
func AppendTasks(ctx context.Context, d *Deps, chainSched *chainsched.CurioChainSched, active *[]harmonytask.TaskInterface) (*servicedeps.Deps, error) {
	bundle, err := buildPDPTasks(ctx, d, chainSched, false)
	if err != nil {
		return nil, err
	}
	*active = append(*active, bundle.tasks...)
	return &servicedeps.Deps{
		EthSender: bundle.ethSender,
		AlertTask: bundle.amTask,
	}, nil
}
