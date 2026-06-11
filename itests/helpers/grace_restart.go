package helpers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/gracehttpsvc"
	"github.com/filecoin-project/curio/pdp"
)

const (
	GraceRestartWorkerEnv     = "CURIO_GRACE_RESTART_WORKER"
	GraceRestartListenFile    = "listen.addr"
	GraceRestartPIDFile       = "curio.pid"
	graceRestartWorkerTimeout = 3 * time.Minute
)

// PDPPingURL returns the PDP ping endpoint URL for a market HTTP base URL.
func PDPPingURL(baseURL string) string {
	return baseURL + pdp.PDPRoutePath + "/ping"
}

// PDPPing checks GET /pdp/ping and returns an error when the response is not 200 OK.
func PDPPing(ctx context.Context, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PDPPingURL(baseURL), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pdp ping returned status %d", resp.StatusCode)
	}
	return nil
}

// WaitForPDPPing blocks until GET /pdp/ping succeeds.
func WaitForPDPPing(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	require.NoError(t, WaitForCondition(ctx, timeout, 100*time.Millisecond, func() (bool, error) {
		err := PDPPing(ctx, baseURL)
		return err == nil, nil
	}))
}

// GraceRestartBaseURL reads the worker listen address file and returns an HTTP base URL.
func GraceRestartBaseURL(repoPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, GraceRestartListenFile))
	if err != nil {
		return "", err
	}
	addr := string(data)
	if len(addr) > 0 && addr[len(addr)-1] == '\n' {
		addr = addr[:len(addr)-1]
	}
	if addr == "" {
		return "", fmt.Errorf("empty listen address in %s", GraceRestartListenFile)
	}
	return "http://" + loopbackDialAddr(addr), nil
}

// RunGraceRestartWorker starts a subprocess-friendly gracehttp server exposing the
// real PDP /pdp/ping handler. It is invoked when CURIO_GRACE_RESTART_WORKER=1.
func RunGraceRestartWorker(t *testing.T) {
	t.Helper()

	repoPath := os.Getenv("CURIO_REPO_PATH")
	require.NotEmpty(t, repoPath)

	listenAddr := FreeListenAddr(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, GraceRestartListenFile), []byte(listenAddr+"\n"), 0644))

	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pdsvc := pdp.NewPDPService(ctx, db, nil, nil, nil, nil, nil, nil)

	router := chi.NewRouter()
	pdp.Routes(router, pdsvc)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}

	pidPath := filepath.Join(repoPath, GraceRestartPIDFile)
	require.NoError(t, gracehttpsvc.WritePIDFile(pidPath))
	defer gracehttpsvc.RemovePIDFile(pidPath)

	require.NoError(t, gracehttpsvc.Serve(server))
}

// StartGraceRestartWorker launches a child test process that serves /pdp/ping via gracehttp.
func StartGraceRestartWorker(t *testing.T, repoPath string) *os.Process {
	t.Helper()

	require.NoError(t, os.MkdirAll(repoPath, 0755))

	cmd := os.Args[0]
	args := []string{
		"-test.run=^TestGraceRestartDuringPDPPing$",
		"-test.count=1",
	}

	procAttr := &os.ProcAttr{
		Dir: repoPath,
		Env: append(os.Environ(),
			GraceRestartWorkerEnv+"=1",
			"CURIO_REPO_PATH="+repoPath,
			"CURIO_SKIP_ALREADY_RUNNING=1",
		),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   nil,
	}

	process, err := os.StartProcess(cmd, append([]string{cmd}, args...), procAttr)
	require.NoError(t, err)

	t.Cleanup(func() {
		if process != nil {
			_ = gracehttpsvc.ShutdownPID(process.Pid)
			_, _ = process.Wait()
		}
	})

	return process
}
