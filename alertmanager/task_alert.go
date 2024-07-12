// Nobody associated with this software's development has any business relationship to pagerduty.
// This is provided as a convenient trampoline to SP's alert system of choice.

package alertmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/filecoin-project/curio/alertmanager/plugin"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/dline"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

const AlertMangerInterval = time.Hour

var log = logging.Logger("curio/alertmanager")

type AlertAPI interface {
	ctladdr.NodeApi
	ChainHead(context.Context) (*types.TipSet, error)
	ChainGetTipSet(context.Context, types.TipSetKey) (*types.TipSet, error)
	StateMinerInfo(ctx context.Context, actor address.Address, tsk types.TipSetKey) (api.MinerInfo, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]api.Partition, error)
}

type AlertTask struct {
	api     AlertAPI
	cfg     config.CurioAlertingConfig
	db      *harmonydb.DB
	plugins []plugin.Plugin
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

type alertFunc func(al *alerts)

var alertFuncs = []alertFunc{
	balanceCheck,
	taskFailureCheck,
	permanentStorageCheck,
	wdPostCheck,
	wnPostCheck,
}

func NewAlertTask(api AlertAPI, db *harmonydb.DB, alertingCfg config.CurioAlertingConfig) *AlertTask {
	return &AlertTask{
		api:     api,
		db:      db,
		cfg:     alertingCfg,
		plugins: plugin.LoadAlertPlugins(alertingCfg),
	}
}

func (a *AlertTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if len(a.plugins) == 0 {
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

	for _, al := range alertFuncs {
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
		for _, ap := range a.plugins {
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
		Max:  1,
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
