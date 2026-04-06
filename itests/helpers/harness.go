package helpers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	logging "github.com/ipfs/go-log/v2"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/cmd/curio/tasks"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/seal"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/node"
)

type CurioLogLevel struct {
	Subsystem string
	Level     string
}

type CurioHarnessOptions struct {
	Layers    []string
	LogLevels []CurioLogLevel
}

type CurioHarness struct {
	API             api.Curio
	Dependencies    *deps.Deps
	EngineTerminate func()
	ClientCloser    jsonrpc.ClientCloser
	FinishCh        <-chan struct{}
}

const defaultHarnessShutdownTimeout = 30 * time.Second

func StartCurioHarness(
	ctx context.Context,
	t *testing.T,
	dir string,
	db *harmonydb.DB,
	idx *indexstore.IndexStore,
	full v1api.FullNode,
	storageRPCSecret string,
	opts CurioHarnessOptions,
) CurioHarness {
	t.Helper()

	ffiselect.IsTest = true
	seal.SetDevnet(true)

	cctx, err := CreateCliContext(dir, opts.Layers)
	require.NoError(t, err)

	shutdownChan := make(chan struct{})
	{
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		go func() {
			<-shutdownChan
			cancel()
		}()
	}

	dependencies := &deps.Deps{
		DB:         db,
		Chain:      full,
		IndexStore: idx,
	}

	require.NoError(t, os.Setenv("CURIO_REPO_PATH", dir))
	require.NoError(t, dependencies.PopulateRemainingDeps(ctx, cctx, false))

	taskEngine, err := tasks.StartTasks(ctx, dependencies, shutdownChan)
	require.NoError(t, err)

	go func() {
		err := rpc.ListenAndServe(ctx, dependencies, shutdownChan)
		if err != nil {
			t.Errorf("failed to start the Curio RPC server: %v", err)
		}
	}()

	finishCh := node.MonitorShutdown(shutdownChan)

	var machines []string
	require.NoError(t, db.Select(ctx, &machines, `select host_and_port from harmony_machines`))
	require.Len(t, machines, 1)
	WaitForTCP(t, machines[0], 30*time.Second)

	laddr, err := net.ResolveTCPAddr("tcp", machines[0])
	require.NoError(t, err)
	ma, err := manet.FromNetAddr(laddr)
	require.NoError(t, err)

	type jwtPayload struct {
		Allow []auth.Permission
	}

	sk, err := base64.StdEncoding.DecodeString(storageRPCSecret)
	require.NoError(t, err)

	apiToken, err := jwt.Sign(&jwtPayload{Allow: lapi.AllPermissions}, jwt.NewHS256(sk))
	require.NoError(t, err)

	require.NoError(t, os.Setenv("CURIO_API_INFO", fmt.Sprintf("%s:%s", string(apiToken), ma)))

	capi, closer, err := rpc.GetCurioAPI(&cli.Context{})
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

	require.NoError(t, capi.StorageInit(ctx, dir, scfg))
	require.NoError(t, capi.StorageAddLocal(ctx, dir))

	for _, ll := range opts.LogLevels {
		_ = logging.SetLogLevel(ll.Subsystem, ll.Level)
	}

	return CurioHarness{
		API:             capi,
		Dependencies:    dependencies,
		EngineTerminate: taskEngine.GracefullyTerminate,
		ClientCloser:    closer,
		FinishCh:        finishCh,
	}
}

func AttachCurioHarnessCleanup(t *testing.T, harness CurioHarness, shutdownTimeout time.Duration) {
	t.Helper()

	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultHarnessShutdownTimeout
	}

	t.Cleanup(harness.EngineTerminate)
	t.Cleanup(harness.ClientCloser)
	t.Cleanup(func() {
		_ = harness.API.Shutdown(context.Background())
		select {
		case <-harness.FinishCh:
		case <-time.After(shutdownTimeout):
			t.Fatalf("timeout waiting for harness shutdown after %s", shutdownTimeout)
		}
	})
}

func StartCurioHarnessWithCleanup(
	ctx context.Context,
	t *testing.T,
	dir string,
	db *harmonydb.DB,
	idx *indexstore.IndexStore,
	full v1api.FullNode,
	storageRPCSecret string,
	opts CurioHarnessOptions,
) CurioHarness {
	t.Helper()

	harness := StartCurioHarness(ctx, t, dir, db, idx, full, storageRPCSecret, opts)
	AttachCurioHarnessCleanup(t, harness, 0)
	return harness
}
