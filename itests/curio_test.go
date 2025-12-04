package itests

import (
	"context"
	"database/sql"
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
	"github.com/filecoin-project/curio/harmony/harmonydb/testutil"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/seal"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/spcli/createminer"
	"github.com/filecoin-project/lotus/itests/kit"
	"github.com/filecoin-project/lotus/node"
)

func TestCurioHappyPath(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	full, miner, esemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
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

	sharedITestID := testutil.SetupTestDB(t)
	t.Logf("sharedITestID: %s", sharedITestID)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)

	defer db.ITestDeleteAll()

	idxStore := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, config.DefaultCurioConfig())
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	var titles []string
	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	require.NoError(t, err)
	require.NotEmpty(t, titles)
	require.NotContains(t, titles, "base")

	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)

	maddr, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)

	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi)
	require.NoError(t, err)

	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	require.NoError(t, err)
	require.Contains(t, titles, "base")
	baseCfg := config.DefaultCurioConfig()
	var baseText string

	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)

	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	require.NotNil(t, baseCfg.Addresses)
	require.GreaterOrEqual(t, len(baseCfg.Addresses.Get()), 1)

	require.Contains(t, baseCfg.Addresses.Get()[0].MinerAddresses, maddr.String())

	baseCfg.Batching.PreCommit.Timeout = time.Second
	baseCfg.Batching.Commit.Timeout = time.Second

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

	capi, enginerTerm, closure, finishCh := ConstructCurioTest(ctx, t, dir, db, idxStore, full, maddr, baseCfg)
	defer enginerTerm()
	defer closure()

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)

	wpt := mi.WindowPoStProofType
	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, false)
	require.NoError(t, err)

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		num, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
		if err != nil {
			return false, err
		}
		require.Len(t, num, 1)

		for _, n := range num {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}

		return true, nil
	})

	require.NoError(t, err)
	require.True(t, comm)

	spt, err = miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, true)
	require.NoError(t, err)

	comm, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		num, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
		if err != nil {
			return false, err
		}
		require.Len(t, num, 1)

		for _, n := range num {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}

		return true, nil
	})
	require.NoError(t, err)
	require.True(t, comm)

	// TODO: add DDO deal, f05 deal 2 MiB each in the sector

	var pollTask []struct {
		SpID                int64                   `db:"sp_id"`
		SectorNumber        int64                   `db:"sector_number"`
		RegisteredSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`

		TicketEpoch *int64 `db:"ticket_epoch"`

		TaskSDR  *int64 `db:"task_id_sdr"`
		AfterSDR bool   `db:"after_sdr"`

		TaskTreeD  *int64 `db:"task_id_tree_d"`
		AfterTreeD bool   `db:"after_tree_d"`

		TaskTreeC  *int64 `db:"task_id_tree_c"`
		AfterTreeC bool   `db:"after_tree_c"`

		TaskTreeR  *int64 `db:"task_id_tree_r"`
		AfterTreeR bool   `db:"after_tree_r"`

		TaskSynth  *int64 `db:"task_id_synth"`
		AfterSynth bool   `db:"after_synth"`

		PreCommitReadyAt *time.Time `db:"precommit_ready_at"`

		TaskPrecommitMsg  *int64 `db:"task_id_precommit_msg"`
		AfterPrecommitMsg bool   `db:"after_precommit_msg"`

		AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
		SeedEpoch                *int64 `db:"seed_epoch"`

		TaskPoRep  *int64 `db:"task_id_porep"`
		PoRepProof []byte `db:"porep_proof"`
		AfterPoRep bool   `db:"after_porep"`

		TaskFinalize  *int64 `db:"task_id_finalize"`
		AfterFinalize bool   `db:"after_finalize"`

		TaskMoveStorage  *int64 `db:"task_id_move_storage"`
		AfterMoveStorage bool   `db:"after_move_storage"`

		CommitReadyAt *time.Time `db:"commit_ready_at"`

		TaskCommitMsg  *int64 `db:"task_id_commit_msg"`
		AfterCommitMsg bool   `db:"after_commit_msg"`

		AfterCommitMsgSuccess bool `db:"after_commit_msg_success"`

		Failed       bool   `db:"failed"`
		FailedReason string `db:"failed_reason"`

		StartEpoch sql.NullInt64 `db:"start_epoch"`
	}

	require.Eventuallyf(t, func() bool {
		h, err := full.ChainHead(ctx)
		require.NoError(t, err)
		t.Logf("head: %d", h.Height())

		err = db.Select(ctx, &pollTask, `
											SELECT 
												sp_id, 
												sector_number, 
												reg_seal_proof, 
												ticket_epoch,
												task_id_sdr, 
												after_sdr,
												task_id_tree_d, 
												after_tree_d,
												task_id_tree_c, 
												after_tree_c,
												task_id_tree_r, 
												after_tree_r,
												task_id_synth, 
												after_synth,
												precommit_ready_at,
												task_id_precommit_msg, 
												after_precommit_msg,
												after_precommit_msg_success, 
												seed_epoch,
												task_id_porep, 
												porep_proof, 
												after_porep,
												task_id_finalize, 
												after_finalize,
												task_id_move_storage, 
												after_move_storage,
												commit_ready_at,
												task_id_commit_msg, 
												after_commit_msg,
												after_commit_msg_success,
												failed, 
												failed_reason,
												start_epoch
											FROM 
												sectors_sdr_pipeline;`)
		require.NoError(t, err)
		for i, task := range pollTask {
			t.Logf("Task %d: SpID=%d, SectorNumber=%d, RegisteredSealProof=%d, TicketEpoch=%s, TaskSDR=%s, AfterSDR=%t, TaskTreeD=%s, AfterTreeD=%t, TaskTreeC=%s, AfterTreeC=%t, TaskTreeR=%s, AfterTreeR=%t, TaskSynth=%s, AfterSynth=%t, PreCommitReadyAt=%s, TaskPrecommitMsg=%s, AfterPrecommitMsg=%t, AfterPrecommitMsgSuccess=%t, SeedEpoch=%s, TaskPoRep=%s, AfterPoRep=%t, TaskFinalize=%s, AfterFinalize=%t, TaskMoveStorage=%s, AfterMoveStorage=%t, CommitReadyAt=%s, TaskCommitMsg=%s, AfterCommitMsg=%t, AfterCommitMsgSuccess=%t, Failed=%t, FailedReason=%s, StartEpoch=%s",
				i,
				task.SpID,
				task.SectorNumber,
				task.RegisteredSealProof,
				valueOrNA(task.TicketEpoch),
				valueOrNA(task.TaskSDR),
				task.AfterSDR,
				valueOrNA(task.TaskTreeD),
				task.AfterTreeD,
				valueOrNA(task.TaskTreeC),
				task.AfterTreeC,
				valueOrNA(task.TaskTreeR),
				task.AfterTreeR,
				valueOrNA(task.TaskSynth),
				task.AfterSynth,
				timeValueOrNA(task.PreCommitReadyAt),
				valueOrNA(task.TaskPrecommitMsg),
				task.AfterPrecommitMsg,
				task.AfterPrecommitMsgSuccess,
				valueOrNA(task.SeedEpoch),
				valueOrNA(task.TaskPoRep),
				task.AfterPoRep,
				valueOrNA(task.TaskFinalize),
				task.AfterFinalize,
				valueOrNA(task.TaskMoveStorage),
				task.AfterMoveStorage,
				timeValueOrNA(task.CommitReadyAt),
				valueOrNA(task.TaskCommitMsg),
				task.AfterCommitMsg,
				task.AfterCommitMsgSuccess,
				task.Failed,
				task.FailedReason,
				nullInt64OrNA(task.StartEpoch),
			)
		}
		return pollTask[0].AfterCommitMsgSuccess && pollTask[1].AfterCommitMsgSuccess
	}, 10*time.Minute, 2*time.Second, "sector did not finish sealing in 10 minutes")

	require.Equal(t, pollTask[0].SectorNumber, int64(0))
	require.Equal(t, pollTask[0].SpID, int64(mid))
	require.Equal(t, pollTask[1].SectorNumber, int64(1))
	require.Equal(t, pollTask[1].SpID, int64(mid))

	_ = capi.Shutdown(ctx)

	<-finishCh
}

func createCliContext(dir string) (*cli.Context, error) {
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
			fmt.Println("Listen address:", c.String("listen"))
			fmt.Println("No-sync:", c.Bool("nosync"))
			fmt.Println("Halt after init:", c.Bool("halt-after-init"))
			fmt.Println("Manage file limit:", c.Bool("manage-fdlimit"))
			fmt.Println("Storage config path:", c.String("storage-json"))
			fmt.Println("Journal path:", c.String("journal"))
			fmt.Println("Layers:", c.StringSlice("layers"))
			fmt.Println("Repo Path:", c.String(deps.FlagRepoPath))
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

	// Parse the flags with test values
	err := set.Parse([]string{rflag, "--listen=0.0.0.0:12345", "--nosync", "--manage-fdlimit", "--layers=seal"})
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}

func ConstructCurioTest(ctx context.Context, t *testing.T, dir string, db *harmonydb.DB, idx *indexstore.IndexStore, full v1api.FullNode, maddr address.Address, cfg *config.CurioConfig) (api.Curio, func(), jsonrpc.ClientCloser, <-chan struct{}) {
	ffiselect.IsTest = true

	cctx, err := createCliContext(dir)
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
	seal.SetDevnet(true)
	err = os.Setenv("CURIO_REPO_PATH", dir)
	require.NoError(t, err)
	err = dependencies.PopulateRemainingDeps(ctx, cctx, false)
	require.NoError(t, err)

	taskEngine, err := tasks.StartTasks(ctx, dependencies, shutdownChan)
	require.NoError(t, err)

	go func() {
		err = rpc.ListenAndServe(ctx, dependencies, shutdownChan) // Monitor for shutdown.
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
	_ = logging.SetLogLevel("cu/seal", "DEBUG")

	return capi, taskEngine.GracefullyTerminate, ccloser, finishCh
}

// Helper functions to handle nil or null values
func valueOrNA(ptr *int64) string {
	if ptr == nil {
		return "NA"
	}
	return fmt.Sprintf("%d", *ptr)
}

func timeValueOrNA(t *time.Time) string {
	if t == nil {
		return "NA"
	}
	return t.Format(time.RFC3339)
}

func nullInt64OrNA(nullInt sql.NullInt64) string {
	if !nullInt.Valid {
		return "NA"
	}
	return fmt.Sprintf("%d", nullInt.Int64)
}
