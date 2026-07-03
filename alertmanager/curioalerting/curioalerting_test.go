package curioalerting

import (
	"context"
	"testing"
	"time"

	"github.com/raulk/clock"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestCurioAlertingEventAndConditionLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	as := NewAlertingSystem(db)

	err = as.EmitEvent(ctx, AlertEvent{
		System:    "pay",
		Subsystem: "settlement",
		Message:   "settlement failed",
	})
	require.NoError(t, err)

	var eventKind, eventAlertName string
	var eventSentAt *time.Time
	err = db.QueryRow(ctx, `
		SELECT kind, alert_name, sent_at
		FROM alert_history
		WHERE system = 'pay' AND subsystem = 'settlement'
	`).Scan(&eventKind, &eventAlertName, &eventSentAt)
	require.NoError(t, err)
	require.Equal(t, "event", eventKind)
	require.Equal(t, "pay_settlement", eventAlertName)
	require.Nil(t, eventSentAt)

	condition := AlertCondition{
		System:    "pdp",
		Subsystem: "watcher",
		Condition: "error",
	}

	err = as.ActivateCondition(ctx, condition, "first error")
	require.NoError(t, err)
	err = as.ActivateCondition(ctx, condition, "latest error")
	require.NoError(t, err)

	var activeMessage string
	var activeRepeatCount int64
	err = db.QueryRow(ctx, `
		SELECT message, repeat_count
		FROM alert_conditions
		WHERE system = 'pdp' AND subsystem = 'watcher' AND condition = 'error'
	`).Scan(&activeMessage, &activeRepeatCount)
	require.NoError(t, err)
	require.Equal(t, "latest error", activeMessage)
	require.Equal(t, int64(1), activeRepeatCount)

	err = as.ResolveCondition(ctx, condition)
	require.NoError(t, err)

	var activeCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM alert_conditions
		WHERE system = 'pdp' AND subsystem = 'watcher' AND condition = 'error'
	`).Scan(&activeCount)
	require.NoError(t, err)
	require.Zero(t, activeCount)

	var historyKind, historyAlertName, historyMessage string
	var historyRepeatCount int64
	var conditionCreatedAt, conditionLastSeenAt, conditionResolvedAt time.Time
	err = db.QueryRow(ctx, `
		SELECT kind, alert_name, message,
			condition_created_at, condition_last_seen_at, condition_resolved_at, condition_repeat_count
		FROM alert_history
		WHERE system = 'pdp' AND subsystem = 'watcher' AND condition = 'error'
	`).Scan(&historyKind, &historyAlertName, &historyMessage, &conditionCreatedAt, &conditionLastSeenAt, &conditionResolvedAt, &historyRepeatCount)
	require.NoError(t, err)
	require.Equal(t, "condition", historyKind)
	require.Equal(t, "pdp_watcher_error", historyAlertName)
	require.Equal(t, "latest error", historyMessage)
	require.False(t, conditionCreatedAt.IsZero())
	require.False(t, conditionLastSeenAt.IsZero())
	require.False(t, conditionResolvedAt.IsZero())
	require.Equal(t, int64(1), historyRepeatCount)

	err = as.ResolveCondition(ctx, condition)
	require.NoError(t, err)

	var historyCount int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM alert_history
		WHERE kind = 'condition'
		  AND system = 'pdp'
		  AND subsystem = 'watcher'
		  AND condition = 'error'
	`).Scan(&historyCount)
	require.NoError(t, err)
	require.Equal(t, 1, historyCount)

	err = as.ActivateCondition(ctx, condition, "new lifecycle")
	require.NoError(t, err)
	err = db.QueryRow(ctx, `
		SELECT message, repeat_count
		FROM alert_conditions
		WHERE system = 'pdp' AND subsystem = 'watcher' AND condition = 'error'
	`).Scan(&activeMessage, &activeRepeatCount)
	require.NoError(t, err)
	require.Equal(t, "new lifecycle", activeMessage)
	require.Zero(t, activeRepeatCount)
}

func TestAlertTaskSendsQueuedEventsAndConditions(t *testing.T) {
	realClock := build.Clock
	mockClock := clock.NewMock()
	mockClock.Set(time.Unix(int64(alertmanager.AlertManagerInterval/time.Second)+1, 0))
	build.Clock = mockClock
	t.Cleanup(func() {
		build.Clock = realClock
	})

	tp := &queuedAlertPlugin{}
	oldTestPlugins := plugin.TestPlugins
	plugin.TestPlugins = []plugin.Plugin{
		tp,
	}
	t.Cleanup(func() {
		plugin.TestPlugins = oldTestPlugins
	})

	oldAlertFuncs := alertmanager.AlertFuncs
	alertmanager.AlertFuncs = map[alertmanager.AlertName]alertmanager.AlertFunc{}
	t.Cleanup(func() {
		alertmanager.AlertFuncs = oldAlertFuncs
	})

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	as := NewAlertingSystem(db)
	err = as.EmitEvent(ctx, AlertEvent{
		System:    "pay",
		Subsystem: "settlement",
		Message:   "settlement failed",
	})
	require.NoError(t, err)
	err = as.ActivateCondition(ctx, AlertCondition{
		System:    "pdp",
		Subsystem: "watcher",
		Condition: "error",
	}, "watcher failed")
	require.NoError(t, err)

	at := alertmanager.NewAlertTask(nil, db, config.CurioAlertingConfig{})
	done, err := at.Do(123, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	require.Equal(t, "settlement failed", tp.details["pay_settlement"])
	require.Equal(t, "watcher failed", tp.details["pdp_watcher_error"])

	var eventSentToPlugins bool
	var eventSentAt *time.Time
	err = db.QueryRow(ctx, `
		SELECT sent_to_plugins, sent_at
		FROM alert_history
		WHERE kind = 'event' AND system = 'pay' AND subsystem = 'settlement'
	`).Scan(&eventSentToPlugins, &eventSentAt)
	require.NoError(t, err)
	require.True(t, eventSentToPlugins)
	require.NotNil(t, eventSentAt)

	var conditionLastNotifiedAt *time.Time
	err = db.QueryRow(ctx, `
		SELECT last_notified_at
		FROM alert_conditions
		WHERE system = 'pdp' AND subsystem = 'watcher' AND condition = 'error'
	`).Scan(&conditionLastNotifiedAt)
	require.NoError(t, err)
	require.NotNil(t, conditionLastNotifiedAt)
}

type queuedAlertPlugin struct {
	details map[string]string
}

func (p *queuedAlertPlugin) SendAlert(data *plugin.AlertPayload) error {
	p.details = make(map[string]string, len(data.Details))
	for name, detail := range data.Details {
		p.details[name] = detail.(string)
	}
	return nil
}
