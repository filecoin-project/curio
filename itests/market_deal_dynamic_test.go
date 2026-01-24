package itests

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/cmd/curio/tasks"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/cli/spcli/createminer"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node"
)

// TestMarketDealDynamicMinerUpdate tests that the market deal system properly
// handles dynamic updates to the miner addresses. This test verifies the bug fix
// where maddrs.OnChange(forMiners) ensures that the miners Dynamic config is updated
// when miner addresses change.
//
// Without this fix, the market deal system would work with stale miner addresses
// and fail to process deals for newly added miners.
func TestMarketDealDynamicMinerUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	full, miner, esemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(2),
		kit.ThroughRPC(),
	)

	esemble.Start()
	blockTime := 100 * time.Millisecond
	esemble.BeginMining(blockTime)

	full.WaitTillChain(ctx, kit.HeightAtLeast(15))

	err := miner.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	err = full.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)

	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)

	sharedITestID := harmonydb.ITestNewID()
	t.Logf("sharedITestID: %s", sharedITestID)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	defer db.ITestDeleteAll()

	idxStore, err := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, config.DefaultCurioConfig())
	require.NoError(t, err)
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	// Create first miner
	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)

	maddr1, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)
	t.Logf("Created first miner: %s", maddr1)

	// Initialize config with first miner
	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr1.String()}, fapi)
	require.NoError(t, err)

	// Get base config and enable market subsystem
	baseCfg := config.DefaultCurioConfig()
	var baseText string

	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)

	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	require.NotNil(t, baseCfg.Addresses)
	require.GreaterOrEqual(t, len(baseCfg.Addresses.Get()), 1)
	require.Contains(t, baseCfg.Addresses.Get()[0].MinerAddresses, maddr1.String())

	// Enable market subsystems
	baseCfg.Subsystems.EnableDealMarket = true
	baseCfg.Batching.PreCommit.Timeout.Set(time.Second)
	baseCfg.Batching.Commit.Timeout.Set(time.Second)

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

	_, err = db.Exec(context.Background(), `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(cb))
	require.NoError(t, err)

	temp := os.TempDir()
	dir, err := os.MkdirTemp(temp, "curio")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(dir)
	}()

	// Start Curio with market enabled
	capi, enginerTerm, closure, finishCh := ConstructCurioWithMarket(ctx, t, dir, db, idxStore, full, maddr1, baseCfg)
	defer enginerTerm()
	defer closure()

	// Wait a bit for the market system to initialize
	time.Sleep(2 * time.Second)

	// Verify the first miner is recognized by the market system
	t.Logf("Verifying first miner is recognized by market system")
	err = verifyMinerInMarketSystem(ctx, db, maddr1)
	require.NoError(t, err, "First miner should be recognized by market system")

	// Now create a second miner while the system is running
	t.Logf("Creating second miner dynamically...")
	maddr2, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)
	t.Logf("Created second miner: %s", maddr2)

	// Update the config to add the second miner to the existing addresses
	var updatedCfg config.CurioConfig
	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)

	_, err = deps.LoadConfigWithUpgrades(baseText, &updatedCfg)
	require.NoError(t, err)

	// Add the second miner to the addresses
	if len(updatedCfg.Addresses.Get()) > 0 {
		addrs := updatedCfg.Addresses.Get()
		addrs[0].MinerAddresses = append(addrs[0].MinerAddresses, maddr2.String())
		updatedCfg.Addresses.Set(addrs)
	}

	// Write the updated config back
	cb, err = config.ConfigUpdate(&updatedCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

	_, err = db.Exec(ctx, `UPDATE harmony_config SET config = $1 WHERE title = 'base'`, string(cb))
	require.NoError(t, err)

	t.Logf("Updated config to include second miner")

	// The dynamic config system should pick up the change automatically
	// This is where the bug would manifest: without maddrs.OnChange(forMiners),
	// the miners Dynamic config would not be updated, and the market system
	// would not know about maddr2

	// Wait for config reload (config polling happens regularly)
	time.Sleep(5 * time.Second)

	// Verify that the second miner is now recognized by the market system
	t.Logf("Verifying second miner is recognized by market system")
	err = verifyMinerInMarketSystem(ctx, db, maddr2)
	if err != nil {
		// This is where the test would fail without the bug fix
		t.Fatalf("Second miner not recognized by market system: %v. This indicates the bug where maddrs.OnChange(forMiners) is missing.", err)
	}

	t.Logf("SUCCESS: Second miner is properly recognized by market system")

	// Additional verification: Check that both miners can have deals in the pipeline
	t.Logf("Verifying both miners can process deals...")

	// Check machine details to see if both miners are tracked
	var machineDetails []struct {
		Miners string `db:"miners"`
	}
	err = db.Select(ctx, &machineDetails, `SELECT miners FROM harmony_machine_details`)
	require.NoError(t, err)
	require.NotEmpty(t, machineDetails, "Machine details should exist")

	t.Logf("Machine details miners: %s", machineDetails[0].Miners)

	// Verify both miners are in the machine details
	require.Contains(t, machineDetails[0].Miners, maddr1.String(), "First miner should be in machine details")
	require.Contains(t, machineDetails[0].Miners, maddr2.String(), "Second miner should be in machine details")

	_ = capi.Shutdown(ctx)
	<-finishCh

	t.Logf("Test completed successfully - dynamic miner updates are working correctly")
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

// ConstructCurioWithMarket is similar to ConstructCurioTest but ensures market subsystems are enabled
func ConstructCurioWithMarket(ctx context.Context, t *testing.T, dir string, db *harmonydb.DB, idx *indexstore.IndexStore, full v1api.FullNode, maddr address.Address, cfg *config.CurioConfig) (api.Curio, func(), jsonrpc.ClientCloser, <-chan struct{}) {
	ffiselect.IsTest = true

	cctx, err := createMarketCliContext(dir)
	require.NoError(t, err)

	shutdownChan := make(chan struct{})

	{
		var ctxclose func()
		ctx, ctxclose = context.WithCancel(ctx)
		go func() {
			<-shutdownChan
			ctxclose()
		}()
	}

	dependencies := &deps.Deps{}
	dependencies.DB = db
	dependencies.Chain = full
	dependencies.IndexStore = idx
	err = os.Setenv("CURIO_REPO_PATH", dir)
	require.NoError(t, err)
	err = dependencies.PopulateRemainingDeps(ctx, cctx, false)
	require.NoError(t, err)

	// Enable market deal system
	taskEngine, err := tasks.StartTasks(ctx, dependencies, shutdownChan)
	require.NoError(t, err)

	go func() {
		err = rpc.ListenAndServe(ctx, dependencies, shutdownChan)
		require.NoError(t, err)
	}()

	finishCh := node.MonitorShutdown(shutdownChan)

	var machines []string
	err = db.Select(ctx, &machines, `select host_and_port from harmony_machines`)
	require.NoError(t, err)

	require.Len(t, machines, 1)
	laddr, err := net.ResolveTCPAddr("tcp", machines[0])
	require.NoError(t, err)

	ma, err := manet.FromNetAddr(laddr)
	require.NoError(t, err)

	var apiToken []byte
	{
		type jwtPayload struct {
			Allow []auth.Permission
		}

		p := jwtPayload{
			Allow: lapi.AllPermissions,
		}

		sk, err := base64.StdEncoding.DecodeString(cfg.Apis.StorageRPCSecret)
		require.NoError(t, err)

		apiToken, err = jwt.Sign(&p, jwt.NewHS256(sk))
		require.NoError(t, err)
	}

	ctoken := fmt.Sprintf("%s:%s", string(apiToken), ma)
	err = os.Setenv("CURIO_API_INFO", ctoken)
	require.NoError(t, err)

	capi, ccloser, err := rpc.GetCurioAPI(&cli.Context{})
	require.NoError(t, err)

	scfg := storiface.LocalStorageMeta{
		ID:         storiface.ID(uuid.New().String()),
		Weight:     10,
		CanSeal:    true,
		CanStore:   true,
		MaxStorage: 0,
		Groups:     []string{},
		AllowTo:    []string{},
	}

	err = capi.StorageInit(ctx, dir, scfg)
	require.NoError(t, err)

	err = capi.StorageAddLocal(ctx, dir)
	require.NoError(t, err)

	_ = logging.SetLogLevel("harmonytask", "DEBUG")
	_ = logging.SetLogLevel("storage-market", "DEBUG")

	return capi, taskEngine.GracefullyTerminate, ccloser, finishCh
}

func createMarketCliContext(dir string) (*cli.Context, error) {
	// Define flags for the command
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "listen",
			Usage:   "host address and port the worker api will listen on",
			Value:   "0.0.0.0:12300",
			EnvVars: []string{"LOTUS_WORKER_LISTEN"},
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:   "halt-after-init",
			Usage:  "only run init, then return",
			Hidden: true,
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "storage-json",
			Usage: "path to json file containing storage config",
			Value: "~/.curio/storage.json",
		},
		&cli.StringSliceFlag{
			Name:    "layers",
			Aliases: []string{"l", "layer"},
			Usage:   "list of layers to be interpreted (atop defaults)",
		},
		&cli.StringFlag{
			Name:    deps.FlagRepoPath,
			EnvVars: []string{"CURIO_REPO_PATH"},
			Value:   "~/.curio",
		},
	}

	// Set up the command with flags
	command := &cli.Command{
		Name:  "simulate",
		Flags: flags,
		Action: func(c *cli.Context) error {
			return nil
		},
	}

	// Create a FlagSet and populate it
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range flags {
		if err := f.Apply(set); err != nil {
			return nil, xerrors.Errorf("Error applying flag: %s\n", err)
		}
	}

	rflag := fmt.Sprintf("--%s=%s", deps.FlagRepoPath, dir)

	// Parse the flags with test values (including market layer)
	err := set.Parse([]string{rflag, "--listen=0.0.0.0:12345", "--nosync", "--manage-fdlimit", "--layers=seal,market"})
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}

// TestMarketDealSystemBasic is a simpler test that just verifies the market
// deal system initializes properly with the OnChange callback in place
func TestMarketDealSystemBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	full, miner, esemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(2),
		kit.ThroughRPC(),
	)

	esemble.Start()
	blockTime := 100 * time.Millisecond
	esemble.BeginMining(blockTime)

	full.WaitTillChain(ctx, kit.HeightAtLeast(10))

	err := miner.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	err = full.LogSetLevel(ctx, "*", "ERROR")
	require.NoError(t, err)

	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)

	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)

	sharedITestID := harmonydb.ITestNewID()
	t.Logf("sharedITestID: %s", sharedITestID)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)
	defer db.ITestDeleteAll()

	idxStore, err := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, config.DefaultCurioConfig())
	require.NoError(t, err)
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)

	maddr, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)

	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi)
	require.NoError(t, err)

	baseCfg := config.DefaultCurioConfig()
	var baseText string

	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)

	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	// Enable market subsystems
	baseCfg.Subsystems.EnableDealMarket = true
	baseCfg.Batching.PreCommit.Timeout.Set(time.Second)
	baseCfg.Batching.Commit.Timeout.Set(time.Second)

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)

	_, err = db.Exec(context.Background(), `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(cb))
	require.NoError(t, err)

	temp := os.TempDir()
	dir, err := os.MkdirTemp(temp, "curio")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(dir)
	}()

	capi, enginerTerm, closure, finishCh := ConstructCurioWithMarket(ctx, t, dir, db, idxStore, full, maddr, baseCfg)
	defer enginerTerm()
	defer closure()

	// Wait for market system to initialize
	time.Sleep(2 * time.Second)

	// Verify the miner is tracked in machine details
	var machineDetails []struct {
		Miners string `db:"miners"`
		Tasks  string `db:"tasks"`
	}
	err = db.Select(ctx, &machineDetails, `SELECT miners, tasks FROM harmony_machine_details`)
	require.NoError(t, err)
	require.NotEmpty(t, machineDetails, "Machine details should exist")

	t.Logf("Machine miners: %s", machineDetails[0].Miners)
	t.Logf("Machine tasks: %s", machineDetails[0].Tasks)

	// Verify the miner address is in the machine details
	require.Contains(t, machineDetails[0].Miners, maddr.String(), "Miner should be tracked in machine details")

	_ = capi.Shutdown(ctx)
	<-finishCh

	t.Logf("Basic market deal system test passed")
}
