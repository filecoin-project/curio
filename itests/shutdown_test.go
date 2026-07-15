package itests

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/itests/helpers"
)

func TestCurioCtrlCShutdown(t *testing.T) {
	t.Cleanup(func() {
		signal.Reset(syscall.SIGTERM, syscall.SIGINT, syscall.SIGABRT)
	})

	ctx := t.Context()
	full, _, db, _ := helpers.BootstrapNetworkWithNewMiner(t, ctx, "2KiB")

	baseCfg, err := helpers.SetBaseConfigWithDefaults(t, ctx, db)
	require.NoError(t, err)

	dir := t.TempDir()
	harness := helpers.StartCurioHarnessWithCleanup(ctx, t, dir, db, helpers.NewIndexStore(ctx, t, config.DefaultCurioConfig()), full, baseCfg.Apis.StorageRPCSecret, helpers.CurioHarnessOptions{})

	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(os.Interrupt))

	select {
	case <-harness.FinishCh:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for Curio shutdown after Ctrl-C")
	}
}
