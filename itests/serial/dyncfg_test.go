//go:build serial

package serial

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonydb/testutil"
)

// TestDynamicConfig tests the dynamic configuration change detection.
// NOTE: Cannot run in parallel - EnableChangeDetection starts a background
// goroutine that persists after the test and can interfere with other tests.
func TestDynamicConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	sharedITestID := testutil.SetupTestDB(t)
	cdb, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	databaseContents := &config.CurioConfig{
		HTTP: config.HTTPConfig{
			ListenAddress: "first value",
		},
		Ingest: config.CurioIngestConfig{
			MaxQueueDownload: config.NewDynamic(10),
		},
	}
	// Write a "testcfg" layer to the database with toml for Ingest having MaxQueueDownload set to 10
	require.NoError(t, setTestConfig(ctx, cdb, databaseContents))

	runtimeConfig := config.DefaultCurioConfig()
	err = deps.ApplyLayers(context.Background(), cdb, runtimeConfig, []string{"testcfg"})
	require.NoError(t, err)

	// database config changes
	databaseContents.Ingest.MaxQueueDownload.Set(20)
	databaseContents.HTTP.ListenAddress = "unapplied value"
	require.NoError(t, setTestConfig(ctx, cdb, databaseContents))

	// "Start the server". This will immediately poll for a config update.
	// Get the stop function to properly shut down the goroutine before test cleanup
	stopFn, err := config.EnableChangeDetectionWithContext(ctx, cdb, databaseContents, []string{"testcfg"}, config.FixTOML)
	require.NoError(t, err)

	// Ensure we stop the change monitor BEFORE database cleanup happens
	defer func() {
		cancel() // Signal context cancellation
		stopFn() // Wait for goroutine to exit
	}()

	// Positive Test: the runtime config should have the new value
	require.Eventually(t, func() bool {
		return databaseContents.Ingest.MaxQueueDownload.Get() == 20
	}, 10*time.Second, 100*time.Millisecond)

	// Negative Test: the runtime config should not have the changed static value
	require.Equal(t, runtimeConfig.HTTP.ListenAddress, "first value")

}

func setTestConfig(ctx context.Context, cdb *harmonydb.DB, cfg *config.CurioConfig) error {
	tomlData, err := config.TransparentMarshal(cfg)
	if err != nil {
		return err
	}
	_, err = cdb.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2)
		ON CONFLICT (title) DO UPDATE SET config = EXCLUDED.config`, "testcfg", string(tomlData))
	return err
}
