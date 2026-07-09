// Nobody associated with this software's development has any business relationship to pagerduty.
// This is provided as a convenient trampoline to SP's alert system of choice.

package alertmanager

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"strings"
	"sync"
	"time"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/dline"

	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/ctladdr"
)

const AlertManagerInterval = 5 * time.Minute
const FullAlertInterval = 30 * time.Minute

var log = logging.Logger("curio/alertmanager")

type AlertAPI interface {
	ctladdr.NodeApi
	ChainReadObj(context.Context, cid.Cid) ([]byte, error)
	ChainHasObj(context.Context, cid.Cid) (bool, error)
	ChainPutObj(context.Context, blocks.Block) error
	ChainHead(context.Context) (*types.TipSet, error)
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

	pingMu       sync.Mutex
	pingProblems bool
}

type alertOut struct {
	err         error
	alertString string
}

type alerts struct {
	ctx         context.Context
	api         AlertAPI
	db          *harmonydb.DB
	cfg         config.CurioAlertingConfig
	alertMap    map[AlertName]*alertOut
	minerAddrs  []address.Address
	walletAddrs []address.Address
}

type AlertFunc func(al *alerts)

type alertMute struct {
	AlertName string  `db:"alert_name"`
	Pattern   *string `db:"pattern"`
}

type pendingAlertEvent struct {
	ID        int64  `db:"id"`
	AlertName string `db:"alert_name"`
	Message   string `db:"message"`
}

type pendingAlertCondition struct {
	System      string `db:"system"`
	Subsystem   string `db:"subsystem"`
	Condition   string `db:"condition"`
	Message     string `db:"message"`
	RepeatCount int64  `db:"repeat_count"`
}

type alertConditionKey struct {
	system    string
	subsystem string
	condition string
}

type queuedAlertRefs struct {
	localEventIDs []int64
	sentEventIDs  []int64

	localConditions []alertConditionKey
	sentConditions  []alertConditionKey
}

type AlertName string

const (
	Name_BalanceCheck          AlertName = "Balance Check"
	Name_TaskFailures          AlertName = "TaskFailures"
	Name_PDPTaskFailures       AlertName = "PDPTaskFailures"
	Name_PermanentStorageSpace AlertName = "PermanentStorageSpace"
	Name_WindowPost            AlertName = "WindowPost"
	Name_WinningPost           AlertName = "WinningPost"
	Name_NowCheck              AlertName = "NowCheck"
	Name_ChainSync             AlertName = "ChainSync"
	Name_MissingSectors        AlertName = "MissingSectors"
	Name_PendingMessages       AlertName = "PendingMessages"
	Name_IPNISync              AlertName = "IPNISync"
	Name_PDPKeyConfigured      AlertName = "PDPKeyConfigured"
)

var AlertNames = []string{
	string(Name_BalanceCheck),
	string(Name_TaskFailures),
	string(Name_PDPTaskFailures),
	string(Name_PermanentStorageSpace),
	string(Name_WindowPost),
	string(Name_WinningPost),
	string(Name_NowCheck),
	string(Name_ChainSync),
	string(Name_MissingSectors),
	string(Name_PendingMessages),
	string(Name_IPNISync),
	string(Name_PDPKeyConfigured),
}

func init() {
	registerAlertMaps()
}

func isPingHealthOnly(now time.Time) bool {
	return now.Unix()%int64(FullAlertInterval.Seconds()) < int64(AlertManagerInterval.Seconds())
}
func funcsByInterval(now time.Time) iter.Seq[AlertFunc] {
	if isPingHealthOnly(now) {
		return maps.Values(PingHealthFuncs)
	}
	return maps.Values(AlertFuncs)
}

func NewAlertTask(
	api AlertAPI, db *harmonydb.DB, alertingCfg config.CurioAlertingConfig) *AlertTask {

	plugins := plugin.LoadAlertPlugins(alertingCfg)

	return &AlertTask{
		api:     api,
		db:      db,
		cfg:     alertingCfg,
		plugins: plugins,
	}
}

// Problems returns the ping-relevant alert results from the most recent
// AlertTask run (ChainSync, PermanentStorageSpace, Balance Check).
// Returns nil before the first run — the node is assumed healthy until
// the first periodic check completes.
func (a *AlertTask) Problems() bool {
	a.pingMu.Lock()
	defer a.pingMu.Unlock()
	return a.pingProblems
}

func (a *AlertTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	now := build.Clock.Now()
	ctx := context.Background()
	altrs := &alerts{
		ctx:      ctx,
		api:      a.api,
		db:       a.db,
		cfg:      a.cfg,
		alertMap: map[AlertName]*alertOut{},
	}

	err = altrs.getAddresses()
	if err != nil {
		return false, xerrors.Errorf("getting addresses: %w", err)
	}

	for al := range funcsByInterval(now) {
		al(altrs)
	}

	// Update the ping-relevant subset for the /ping health endpoint.
	for name := range maps.Keys(PingHealthFuncs) {
		out, ok := altrs.alertMap[name]
		if !ok || out == nil || out.err == nil || out.alertString == "" {
			continue
		}
		// Only say unhealthy if PDP IPNI sync is failing, PoRep provider should not fail health check
		if name == Name_IPNISync {
			if strings.Contains(out.alertString, "PDP") {
				log.Warnf("Ping health check problem: %s %s %s", name, out.err, out.alertString)
				a.pingMu.Lock()
				a.pingProblems = true
				a.pingMu.Unlock()
			}
		} else {
			log.Warnf("Ping health check problem: %s %s %s", name, out.err, out.alertString)
			a.pingMu.Lock()
			a.pingProblems = true
			a.pingMu.Unlock()
		}
		break
	}
	if isPingHealthOnly(now) {
		return true, nil
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

	details := make(map[string]any)

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
			muted := isAlertMuted(string(k), alertMsg, mutes)
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
				details[string(k)] = alertMsg
			}
		}
	}

	queuedRefs, err := a.collectQueuedAlerts(ctx, mutes, details)
	if err != nil {
		return false, err
	}
	if err := a.markQueuedAlertsProcessed(ctx, now, queuedRefs.localEventIDs, queuedRefs.localConditions, false); err != nil {
		return false, err
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

		if err := a.markQueuedAlertsProcessed(ctx, now, queuedRefs.sentEventIDs, queuedRefs.sentConditions, true); err != nil {
			return false, err
		}

		if len(errs) != 0 {
			return false, fmt.Errorf("errors sending alerts: %v", errs)
		}
	} else if len(a.plugins) == 0 && len(details) > 0 {
		log.Warnf("No alert plugins enabled, alerts recorded to DB but not sent externally")
	}

	return true, nil

}

func (a *AlertTask) collectQueuedAlerts(ctx context.Context, mutes []alertMute, details map[string]any) (*queuedAlertRefs, error) {
	hasPlugins := len(a.plugins) > 0
	refs := &queuedAlertRefs{}

	var events []pendingAlertEvent
	err := a.db.Select(ctx, &events, `
		SELECT id, alert_name, message
		FROM alert_history
		WHERE kind = 'event'
		  AND sent_at IS NULL
		ORDER BY created_at
	`)
	if err != nil {
		return nil, xerrors.Errorf("loading queued alert events: %w", err)
	}

	for _, event := range events {
		muted := isAlertMuted(event.AlertName, event.Message, mutes)
		if muted {
			log.Debugf("Queued alert event %s muted: %s", event.AlertName, event.Message)
			refs.localEventIDs = append(refs.localEventIDs, event.ID)
			continue
		}

		addAlertDetail(details, event.AlertName, event.Message)
		if hasPlugins {
			refs.sentEventIDs = append(refs.sentEventIDs, event.ID)
		} else {
			refs.localEventIDs = append(refs.localEventIDs, event.ID)
		}
	}

	var conditions []pendingAlertCondition
	err = a.db.Select(ctx, &conditions, `
		SELECT system, subsystem, condition, message, repeat_count
		FROM alert_conditions
		WHERE last_notified_at IS NULL
		ORDER BY created_at
	`)
	if err != nil {
		return nil, xerrors.Errorf("loading queued alert conditions: %w", err)
	}

	for _, condition := range conditions {
		alertName := conditionAlertName(condition.System, condition.Subsystem, condition.Condition)
		key := alertConditionKey{
			system:    condition.System,
			subsystem: condition.Subsystem,
			condition: condition.Condition,
		}

		muted := isAlertMuted(alertName, condition.Message, mutes)
		if muted {
			log.Debugf("Queued alert condition %s muted: %s", alertName, condition.Message)
			refs.localConditions = append(refs.localConditions, key)
			continue
		}

		addAlertDetail(details, alertName, condition.Message)
		if hasPlugins {
			refs.sentConditions = append(refs.sentConditions, key)
		} else {
			refs.localConditions = append(refs.localConditions, key)
		}
	}

	return refs, nil
}

func (a *AlertTask) markQueuedAlertsProcessed(ctx context.Context, now time.Time, eventIDs []int64, conditions []alertConditionKey, sentToPlugins bool) error {
	if len(eventIDs) > 0 {
		_, err := a.db.Exec(ctx, `
			UPDATE alert_history
			SET sent_to_plugins = $1,
				sent_at = $2
			WHERE id = ANY($3)
			  AND kind = 'event'
			  AND sent_at IS NULL
		`, sentToPlugins, now, eventIDs)
		if err != nil {
			return xerrors.Errorf("marking queued alert events processed: %w", err)
		}
	}

	for _, condition := range conditions {
		_, err := a.db.Exec(ctx, `
			UPDATE alert_conditions
			SET last_notified_at = $4
			WHERE system = $1
			  AND subsystem = $2
			  AND condition = $3
			  AND last_notified_at IS NULL
		`, condition.system, condition.subsystem, condition.condition, now)
		if err != nil {
			return xerrors.Errorf("marking queued alert condition processed: %w", err)
		}
	}

	return nil
}

const maxAlertDetailLength = 1000

const alertDetailTruncatedSuffix = " ... (truncated, see alert_history for full list)"

func addAlertDetail(details map[string]any, alertName string, alertMessage string) {
	existing, ok := details[alertName]
	if !ok {
		details[alertName] = alertMessage
		return
	}

	existingStr, _ := existing.(string)
	if strings.HasSuffix(existingStr, alertDetailTruncatedSuffix) {
		return
	}

	combined := fmt.Sprintf("%s. %s", existingStr, alertMessage)
	if len(combined) > maxAlertDetailLength {
		details[alertName] = existingStr + alertDetailTruncatedSuffix
		return
	}
	details[alertName] = combined
}

func conditionAlertName(system, subsystem, condition string) string {
	return system + "_" + subsystem + "_" + condition
}

// isAlertMuted checks if an alert matches any active mute patterns
func isAlertMuted(alertName string, alertMessage string, mutes []alertMute) bool {
	for _, mute := range mutes {
		if mute.AlertName != alertName && mute.AlertName != "others" {
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
			Cpu: 0,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(AlertManagerInterval, a),
	}
}

func (a *AlertTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &AlertTask{}
var _ = harmonytask.Reg(&AlertTask{})
