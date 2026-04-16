package itests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/itests/helpers"
)

func TestDynamicConfig(t *testing.T) {
	ctx := t.Context()

	sharedITestID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID, true)
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
	require.NoError(t, config.EnableChangeDetection(cdb, runtimeConfig, []string{"testcfg"}, config.FixTOML))

	// Positive Test: the runtime config should have the new value
	require.Eventually(t, func() bool {
		return runtimeConfig.Ingest.MaxQueueDownload.Get() == 20
	}, 10*time.Second, 100*time.Millisecond)

	// Negative Test: the runtime config should not have the changed static value
	require.Equal(t, runtimeConfig.HTTP.ListenAddress, "first value")

}

func setTestConfig(ctx context.Context, cdb *harmonydb.DB, cfg *config.CurioConfig) error {
	tomlData, err := config.TransparentMarshal(cfg)
	if err != nil {
		return err
	}
	_, err = cdb.Exec(ctx, `
		INSERT INTO harmony_config (title, config) VALUES ($1, $2)
		ON CONFLICT (title) DO UPDATE
		SET config = EXCLUDED.config, timestamp = NOW()
	`, "testcfg", string(tomlData))
	return err
}

// verifyMinerInMarketSystem checks if a miner is properly recognized by the market system
// by checking if the miner's info is available in the database and market handlers
func verifyMinerInMarketSystem(ctx context.Context, db *harmonydb.DB, maddr address.Address) error {
	// Check if we can query sector sizes for this miner
	// The market system needs to know the sector size for each miner

	// Try to check if there's any market configuration for this miner
	var count int
	err := db.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM harmony_machine_details 
		WHERE miners LIKE $1
	`, "%"+maddr.String()+"%").Scan(&count)

	if err != nil {
		return xerrors.Errorf("failed to query machine details: %w", err)
	}

	if count == 0 {
		return xerrors.Errorf("miner %s not found in machine details", maddr)
	}

	return nil
}

// TestMarketDynamicConfigMinerUpdate verifies the market system picks up miner
// address changes from dynamic config while running.
func TestMarketDynamicConfigMinerUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	full, miner, db, maddr1 := helpers.BootstrapNetworkWithNewMiner(t, ctx, "2KiB")
	defer db.ITestDeleteAll()

	baseCfg, err := helpers.SetBaseConfigWithDefaults(t, ctx, db)
	require.NoError(t, err)

	require.NotNil(t, baseCfg.Addresses)
	require.GreaterOrEqual(t, len(baseCfg.Addresses.Get()), 1)
	require.Contains(t, baseCfg.Addresses.Get()[0].MinerAddresses, maddr1.String())

	dir, err := os.MkdirTemp(os.TempDir(), "curio")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(dir)
	}()

	helpers.StartCurioHarnessWithCleanup(ctx, t, dir, db, helpers.NewIndexStore(ctx, t, baseCfg), full, baseCfg.Apis.StorageRPCSecret, helpers.CurioHarnessOptions{
		LogLevels: []helpers.CurioLogLevel{
			{Subsystem: "harmonytask", Level: "DEBUG"},
			{Subsystem: "storage-market", Level: "DEBUG"},
		},
	})

	err = verifyMinerInMarketSystem(ctx, db, maddr1)
	require.NoError(t, err, "first miner should be recognized by market system")

	var machineDetails []struct {
		Miners string `db:"miners"`
		Tasks  string `db:"tasks"`
	}
	err = db.Select(ctx, &machineDetails, `SELECT miners, tasks FROM harmony_machine_details`)
	require.NoError(t, err)
	require.NotEmpty(t, machineDetails, "machine details should exist")
	require.Contains(t, machineDetails[0].Miners, maddr1.String(), "first miner should be in machine details")

	maddr2, err := helpers.CreateStorageMiner(ctx, full, miner.OwnerKey.Address, "2KiB", 0)
	require.NoError(t, err)

	updatedCfg, err := helpers.LoadBaseConfigFromDB(ctx, db)
	require.NoError(t, err)

	if len(updatedCfg.Addresses.Get()) > 0 {
		addrs := updatedCfg.Addresses.Get()
		addrs[0].MinerAddresses = append(addrs[0].MinerAddresses, maddr2.String())
		updatedCfg.Addresses.Set(addrs)
	}
	require.NoError(t, helpers.UpdateBaseConfigWithTimestamp(ctx, db, updatedCfg))

	require.Eventually(t, func() bool {
		return verifyMinerInMarketSystem(ctx, db, maddr2) == nil
	}, 90*time.Second, 1*time.Second, "second miner not recognized by market system; dynamic miner update callback may be missing")

	err = db.Select(ctx, &machineDetails, `SELECT miners, tasks FROM harmony_machine_details`)
	require.NoError(t, err)
	require.NotEmpty(t, machineDetails, "machine details should exist")
	require.Contains(t, machineDetails[0].Miners, maddr1.String(), "first miner should be in machine details")
	require.Contains(t, machineDetails[0].Miners, maddr2.String(), "second miner should be in machine details")
}
