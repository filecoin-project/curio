package itests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestDynamicConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cdb, err := harmonydb.NewFromConfigWithTest(t)
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
	require.NoError(t, config.EnableChangeDetection(cdb, databaseContents, []string{"testcfg"}, config.FixTOML))

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
	_, err = cdb.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2)`, "testcfg", string(tomlData))
	return err
}
