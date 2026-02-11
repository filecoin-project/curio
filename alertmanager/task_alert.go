// Nobody associated with this software's development has any business relationship to pagerduty.
// This is provided as a convenient trampoline to SP's alert system of choice.

package alertmanager

import (
	"context"
	"fmt"
	"strings"
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

const AlertMangerInterval = 30 * time.Minute

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
	plugins []plugin.Plugin
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

type alertMute struct {
	AlertName string  `db:"alert_name"`
	Pattern   *string `db:"pattern"`
}

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

	// Load active mutes from database
	var mutes []alertMute
	err = a.db.Select(ctx, &mutes, `
		SELECT alert_name, pattern
		FROM alert_mutes
		WHERE active = TRUE AND (expires_at IS NULL OR expires_at > NOW())
	`)
	if err != nil {
		log.Errorf("Error loading alert mutes: %s", err)
		// Continue without muting on error
	}

	details := make(map[string]interface{})
	now := time.Now()

	// Process regular alerts
	for k, v := range altrs.alertMap {
		if v != nil {
			var alertMsg string
			if v.err != nil {
				alertMsg = v.err.Error()
			} else if v.alertString != "" {
				alertMsg = v.alertString
			} else {
				continue
			}

			// Check if this alert should be muted
			muted := isAlertMuted(k, alertMsg, mutes)
			if muted {
				log.Debugf("Alert %s muted: %s", k, alertMsg)
			}

			// Always record to alert_history (even if muted, for visibility)
			_, dbErr := a.db.Exec(ctx, `
				INSERT INTO alert_history (alert_name, message, machine_name, sent_to_plugins, sent_at)
				VALUES ($1, $2, $3, $4, $5)
			`, k, alertMsg, nil, !muted && len(a.plugins) > 0, now)
			if dbErr != nil {
				log.Errorf("Failed to record alert to history: %s", dbErr)
			}

			// Only add to details for sending if not muted
			if !muted {
				details[k] = alertMsg
			}
		}
	}

	// Process system alerts (from curioalerting)
	{
		a.al.Lock()
		defer a.al.Unlock()
		for sys, meta := range a.al.Current {
			alertKey := sys.System + "_" + sys.Subsystem
			alertMsg := fmt.Sprintf("%v", meta)

			// Check if system alerts should be muted
			muted := isAlertMuted(alertKey, alertMsg, mutes)
			if muted {
				log.Debugf("System alert %s muted", alertKey)
			}

			// Always record to alert_history
			_, dbErr := a.db.Exec(ctx, `
				INSERT INTO alert_history (alert_name, message, machine_name, sent_to_plugins, sent_at)
				VALUES ($1, $2, $3, $4, $5)
			`, alertKey, alertMsg, nil, !muted && len(a.plugins) > 0, now)
			if dbErr != nil {
				log.Errorf("Failed to record system alert to history: %s", dbErr)
			}

			if !muted {
				details[alertKey] = meta
			}
		}
	}

	// Send to plugins if there are any alerts and plugins configured
	if len(details) > 0 && len(a.plugins) > 0 {
		payloadData := &plugin.AlertPayload{
			Summary:  "Curio Alert",
			Severity: "critical", // This can be critical, error, warning or info.
			Source:   "Curio Cluster",
			Details:  details,
			Time:     now,
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
	} else if len(a.plugins) == 0 && len(details) > 0 {
		log.Warnf("No alert plugins enabled, alerts recorded to DB but not sent externally")
	}

	return true, nil

}

// isAlertMuted checks if an alert matches any active mute patterns
func isAlertMuted(alertName, alertMessage string, mutes []alertMute) bool {
	for _, mute := range mutes {
		if mute.AlertName != alertName {
			continue
		}
		// If no pattern, mute entire category
		if mute.Pattern == nil || *mute.Pattern == "" {
			return true
		}
		// Check if message matches pattern (simple substring match)
		if strings.Contains(alertMessage, *mute.Pattern) {
			return true
		}
	}
	return false
}

func (a *AlertTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
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
