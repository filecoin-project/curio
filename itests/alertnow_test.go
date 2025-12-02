package itests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/alertmanager/plugin"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonydb/testutil"
)

func TestAlertNow(t *testing.T) {
	// TestAlertNow tests alerting system

	tp := &testPlugin{}
	plugin.TestPlugins = []plugin.Plugin{
		tp,
	}
	// Create dependencies
	sharedITestID := testutil.SetupTestDB(t)
	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	an := alertmanager.NewAlertNow(db, "alertNowMachine")
	an.AddAlert("testMessage")

	as := curioalerting.NewAlertingSystem()
	alertmanager.AlertFuncs = []alertmanager.AlertFunc{alertmanager.NowCheck}
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
