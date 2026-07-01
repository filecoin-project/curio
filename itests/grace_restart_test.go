package itests

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/itests/helpers"
	"github.com/filecoin-project/curio/lib/gracehttpsvc"
)

func TestGraceRestartDuringPDPPing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping grace restart integration test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("gracehttp restart integration test requires linux")
	}

	if os.Getenv(helpers.GraceRestartWorkerEnv) == "1" {
		helpers.RunGraceRestartWorker(t)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	repoPath := filepath.Join(t.TempDir(), "grace-restart-repo")
	_ = helpers.StartGraceRestartWorker(t, repoPath)

	baseURL, err := helpers.GraceRestartBaseURL(repoPath)
	require.NoError(t, err)

	helpers.WaitForPDPPing(t, baseURL, 2*time.Minute)

	pidPath := filepath.Join(repoPath, helpers.GraceRestartPIDFile)
	initialPID, err := gracehttpsvc.ReadPID(pidPath)
	require.NoError(t, err)
	require.Greater(t, initialPID, 0)

	var pingFailures atomic.Int64
	var pingSuccesses atomic.Int64
	stopPinger := make(chan struct{})
	pingerDone := make(chan struct{})

	go func() {
		defer close(pingerDone)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stopPinger:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := helpers.PDPPing(ctx, baseURL); err != nil {
					pingFailures.Add(1)
					continue
				}
				pingSuccesses.Add(1)
			}
		}
	}()

	require.Eventually(t, func() bool {
		return pingSuccesses.Load() >= 5
	}, 30*time.Second, 25*time.Millisecond, "expected initial successful PDP pings before restart")

	require.NoError(t, gracehttpsvc.RestartFromPIDFile(pidPath))

	require.Eventually(t, func() bool {
		currentPID, err := gracehttpsvc.ReadPID(pidPath)
		if err != nil {
			return false
		}
		return currentPID != initialPID && currentPID > 0
	}, 2*time.Minute, 100*time.Millisecond, "expected curio.pid to reflect a new process after restart")

	require.Eventually(t, func() bool {
		return pingSuccesses.Load() >= 10
	}, 2*time.Minute, 25*time.Millisecond, "expected continued successful PDP pings through restart")

	close(stopPinger)
	<-pingerDone

	require.Zero(t, pingFailures.Load(), "expected zero failed PDP pings during graceful restart")
}
