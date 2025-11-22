// Nobody associated with this software's development has any business relationship to pagerduty.
// This is provided as a convenient trampoline to SP's alert system of choice.

package alertmanager

import (
	"context"
	"fmt"
	"time"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/dline"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

const AlertMangerInterval = time.Hour

var log = logging.Logger("curio/alertmanager")

type AlertAPI interface {
	ctladdr.NodeApi
	ChainReadObj(context.Context, cid.Cid) ([]byte, error)
	ChainHasObj(context.Context, cid.Cid) (bool, error)
	ChainPutObj(context.Context, blocks.Block) error
	ChainHead(context.Context) (*types.TipSet, error)
	ChainGetTipSet(context.Context, types.TipSetKey) (*types.TipSet, error)
	StateMinerInfo(ctx context.Context, actor address.Address, tsk types.TipSetKey) (api.MinerInfo, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]api.Partition, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
}

type AlertTask struct {
	api     AlertAPI
	cfg     config.CurioAlertingConfig
	db      *harmonydb.DB
	plugins *config.Dynamic[[]plugin.Plugin]
	al      *curioalerting.AlertingSystem
}

type alertOut struct {
	err         error
	alertString string
}

type alerts struct {
	ctx      context.Context
	api      AlertAPI
	db       *harmonydb.DB
	cfg      config.CurioAlertingConfig
	alertMap map[string]*alertOut
}

type AlertFunc func(al *alerts)

var AlertFuncs = []AlertFunc{
	balanceCheck,
	taskFailureCheck,
	permanentStorageCheck,
	wdPostCheck,
	wnPostCheck,
	NowCheck,
	chainSyncCheck,
	missingSectorCheck,
	pendingMessagesCheck,
}

func NewAlertTask(
	api AlertAPI, db *harmonydb.DB, alertingCfg config.CurioAlertingConfig, al *curioalerting.AlertingSystem) *AlertTask {

	plugins := plugin.LoadAlertPlugins(alertingCfg)

	return &AlertTask{
		api:     api,
		db:      db,
		cfg:     alertingCfg,
		al:      al,
		plugins: plugins,
	}
}

func (a *AlertTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if len(a.plugins.Get()) == 0 {
		log.Warnf("No alert plugins enabled, not sending an alert")
		return true, nil
	}

	ctx := context.Background()

	alMap := make(map[string]*alertOut)

	altrs := &alerts{
		ctx:      ctx,
		api:      a.api,
		db:       a.db,
		cfg:      a.cfg,
		alertMap: alMap,
	}

	for _, al := range AlertFuncs {
		al(altrs)
	}

	details := make(map[string]interface{})

	for k, v := range altrs.alertMap {
		if v != nil {
			if v.err != nil {
				details[k] = v.err.Error()
				continue
			}
			if v.alertString != "" {
				details[k] = v.alertString
			}
		}
	}
	{
		a.al.Lock()
		defer a.al.Unlock()
		for sys, meta := range a.al.Current {
			details[sys.System+"_"+sys.Subsystem] = meta
		}
	}

	// Alert only if required
	if len(details) > 0 {
		payloadData := &plugin.AlertPayload{
			Summary:  "Curio Alert",
			Severity: "critical", // This can be critical, error, warning or info.
			Source:   "Curio Cluster",
			Details:  details,
			Time:     time.Now(),
		}

		var errs []error
		for _, ap := range a.plugins.Get() {
			err = ap.SendAlert(payloadData)
			if err != nil {
				log.Errorf("Error sending alert: %s", err)
				errs = append(errs, err)
			}
		}
		if len(errs) != 0 {
			return false, fmt.Errorf("errors sending alerts: %v", errs)
		}
	}

	return true, nil

}

func (a *AlertTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (a *AlertTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "AlertManager",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(AlertMangerInterval, a),
	}
}

func (a *AlertTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &AlertTask{}
var _ = harmonytask.Reg(&AlertTask{})
