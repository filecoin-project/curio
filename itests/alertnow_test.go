package itests

import (
	"testing"
	"time"

	"github.com/raulk/clock"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestAlertNow(t *testing.T) {
	// TestAlertNow tests alerting system
	realClock := build.Clock
	mockClock := clock.NewMock()
	// AlertTask runs only ping-health checks during the first AlertManagerInterval
	// of each FullAlertInterval. Pin this test just after that window so NowCheck
	// is included and the plugin dispatch path is exercised deterministically.
	mockClock.Set(time.Unix(int64(alertmanager.AlertManagerInterval/time.Second)+1, 0))
	build.Clock = mockClock
	t.Cleanup(func() {
		build.Clock = realClock
	})

	tp := &testPlugin{}
	oldTestPlugins := plugin.TestPlugins
	plugin.TestPlugins = []plugin.Plugin{
		tp,
	}
	t.Cleanup(func() {
		plugin.TestPlugins = oldTestPlugins
	})

	// Create dependencies
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	an := alertmanager.NewAlertNow(db, "alertNowMachine")
	an.AddAlert("testMessage")

	as := curioalerting.NewAlertingSystem()
	oldAlertFuncs := alertmanager.AlertFuncs
	alertmanager.AlertFuncs = map[alertmanager.AlertName]alertmanager.AlertFunc{
		alertmanager.Name_NowCheck: alertmanager.NowCheck,
	}
	t.Cleanup(func() {
		alertmanager.AlertFuncs = oldAlertFuncs
	})

	// Create a new alert task
	at := alertmanager.NewAlertTask(nil, db, config.CurioAlertingConfig{}, as)
	done, err := at.Do(123, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, "Machine alertNowMachine: testMessage", tp.output)
}

// testPlugin is a test plugin
type testPlugin struct {
	output string
}

func (tp *testPlugin) SendAlert(data *plugin.AlertPayload) error {
	tp.output = data.Details["NowCheck"].(string)
	return nil
}
