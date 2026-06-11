package pdpnode

import (
	"context"
	"sync"

	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/harmony/harmonytask"
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
	ServiceDeps cuhttp.ServiceDeps
	EthSender   *message.SenderETH
}

type pdpTaskBundle struct {
	tasks     []harmonytask.TaskInterface
	amTask    *alertmanager.AlertTask
	ethSender *message.SenderETH
}

func buildPDPTasks(ctx context.Context, d *Deps, chainSched *chainsched.CurioChainSched) (*pdpTaskBundle, error) {
	cfg := d.Cfg
	db := d.DB

	var tasks []harmonytask.TaskInterface

	_, sendTask := message.NewSender(d.Chain, d.Chain, db, cfg.Fees.MaximizeFeeCap)
	tasks = append(tasks, sendTask)

	var senderEth *message.SenderETH
	var senderEthOnce sync.Once
	getSenderEth := func() *message.SenderETH {
		senderEthOnce.Do(func() {
			ec, err := d.EthClient.Val()
			if err != nil {
				log.Errorw("failed to get eth client", "error", err)
				return
			}
			var ethSenderTask *message.SendTaskETH
			senderEth, ethSenderTask = message.NewSenderETH(ec, db)
			tasks = append(tasks, ethSenderTask)
		})
		return senderEth
	}

	amTask := alertmanager.NewAlertTask(d.Chain, db, cfg.Alerting, d.Al)

	ethClient := must.One(d.EthClient.Val())
	es := getSenderEth()

	pdp.NewWatcherDataSetCreate(db, ethClient, chainSched)
	pdp.NewWatcherPieceAdd(db, chainSched, ethClient)
	pdp.NewWatcherDelete(db, chainSched)
	pdp.NewWatcherPieceDelete(db, chainSched)

	tasks = append(tasks,
		pdp.NewPDPNotifyTask(db),
		pdp.NewProveTask(chainSched, db, ethClient, d.Chain, es, d.CachedPieceReader, d.IndexStore),
		pdp.NewNextProvingPeriodTask(db, ethClient, d.Chain, chainSched, es),
		pdp.NewInitProvingPeriodTask(db, ethClient, d.Chain, chainSched, es),
		pdp.NewPDPCommpTask(db, d.PieceIO, cfg.Subsystems.CommPMaxTasks),
		pdp.NewPDPTaskAddPiece(db, es, ethClient),
		pdp.NewPDPTaskAddDataSet(db, es, ethClient, d.Chain),
		pdp.NewAggregatePDPDealTask(db, d.PieceIO),
		pdp.NewTaskPDPSaveCache(db, d.CachedPieceReader, d.IndexStore),
		pdp.NewPDPTaskDeletePiece(db, es, ethClient),
		pdp.NewPDPTaskDeleteDataSet(db, es, ethClient, d.Chain),
	)

	pdpv0.NewDataSetWatch(db, ethClient, chainSched)
	pay.NewSettleWatcher(db, ethClient, chainSched, d.Al)
	pdpv0.NewDataSetDeleteWatcher(db, ethClient, chainSched)
	pdpv0.NewCleanupPiecesWatcher(db, ethClient, chainSched)
	pdpv0.NewTerminateServiceWatcher(db, ethClient, chainSched)

	tasks = append(tasks,
		pdpv0.NewProveTask(chainSched, db, ethClient, d.Chain, es, d.CachedPieceReader, d.IndexStore),
		pdpv0.NewPDPNotifyTask(ctx, db),
		pdpv0.NewPDPPullPieceTask(ctx, db, d.PieceIO, cfg.Subsystems.PDPPullPieceMaxTasks),
		pdpv0.NewNextProvingPeriodTask(db, ethClient, d.Chain, chainSched, es),
		pdpv0.NewInitProvingPeriodTask(db, ethClient, d.Chain, chainSched, es),
		pdpv0.NewTerminateServiceTask(db, ethClient, es),
		pdpv0.NewDeleteDataSetTask(db, ethClient, es),
		pdpv0.NewCleanupPiecesTask(db, ethClient, es),
		pdpv0.NewTaskChainSync(db, ethClient, es),
		pay.NewSettleTask(db, ethClient, es),
		pdpv0.NewTaskPDPSaveCache(db, d.CachedPieceReader, d.IndexStore),
		pdpv0.NewPieceGCTask(&cfg.HTTP, db, d.IndexStore),
		pdpv0.NewReorgCheckTask(db, ethClient, d.Chain),
	)

	idxMax := taskhelp.Max(cfg.Subsystems.IndexingMaxTasks)
	tasks = append(tasks,
		indexing.NewPDPIndexingTask(db, d.IndexStore, d.CachedPieceReader, cfg, idxMax),
		indexing.NewPDPIPNITask(db, cfg, idxMax, d.IndexStore),
		indexing.NewPDPV0IndexingTask(db, d.IndexStore, d.CachedPieceReader, cfg, idxMax),
		indexing.NewPDPV0IPNITask(db, cfg, idxMax, d.IndexStore),
	)

	tasks = append(tasks, amTask)
	tasks = append(tasks, gc.NewPieceCleanupTask(db, d.IndexStore))

	return &pdpTaskBundle{
		tasks:     tasks,
		amTask:    amTask,
		ethSender: es,
	}, nil
}

// RegisterTasks wires PDP harmony tasks and returns the task engine.
func RegisterTasks(ctx context.Context, d *Deps) (*TaskResult, error) {
	chainSched := chainsched.New(d.Chain)

	bundle, err := buildPDPTasks(ctx, d, chainSched)
	if err != nil {
		return nil, err
	}

	ht, err := harmonytask.New(d.DB, bundle.tasks, d.MachineHost)
	if err != nil {
		return nil, err
	}

	d.MachineID = int64(ht.ResourcesAvailable().MachineID)

	if bundle.ethSender != nil {
		watcherEth, err := message.NewMessageWatcherEth(d.DB, ht, chainSched, must.One(d.EthClient.Val()))
		if err != nil {
			return nil, xerrors.Errorf("eth message watcher: %w", err)
		}
		_ = watcherEth
	}

	go chainSched.Run(ctx)

	return &TaskResult{
		Engine:    ht,
		AlertTask: bundle.amTask,
		EthSender: bundle.ethSender,
		ServiceDeps: cuhttp.ServiceDeps{
			EthSender: bundle.ethSender,
			AlertTask: bundle.amTask,
		},
	}, nil
}

// AppendTasks adds PDP tasks to a curio task list.
func AppendTasks(ctx context.Context, d *Deps, chainSched *chainsched.CurioChainSched, active *[]harmonytask.TaskInterface) (*cuhttp.ServiceDeps, error) {
	bundle, err := buildPDPTasks(ctx, d, chainSched)
	if err != nil {
		return nil, err
	}
	*active = append(*active, bundle.tasks...)
	sd := &cuhttp.ServiceDeps{
		EthSender: bundle.ethSender,
		AlertTask: bundle.amTask,
	}
	return sd, nil
}
