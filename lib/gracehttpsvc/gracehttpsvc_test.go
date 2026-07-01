package gracehttpsvc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePID(t *testing.T) {
	t.Parallel()

	pid, err := parsePID([]byte("12345\n"))
	if err != nil || pid != 12345 {
		t.Fatalf("expected pid 12345, got %d (%v)", pid, err)
	}

	pid, err = parsePID([]byte("99"))
	if err != nil || pid != 99 {
		t.Fatalf("expected pid 99, got %d (%v)", pid, err)
	}

	if _, err := parsePID([]byte("not-a-pid")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestRestartIfAlreadyRunningMissingPIDFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "curio.pid")

	restarted, err := RestartIfAlreadyRunning(pidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restarted {
		t.Fatal("expected no restart when pid file is missing")
	}
}

func TestRestartIfAlreadyRunningStalePIDFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "curio.pid")
	if err := os.WriteFile(pidPath, []byte("999999999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	restarted, err := RestartIfAlreadyRunning(pidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restarted {
		t.Fatal("expected no restart for stale pid")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("expected stale pid file to be removed")
	}
}

func TestRestartIfAlreadyRunningCurrentProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "curio.pid")
	if err := WritePIDFile(pidPath); err != nil {
		t.Fatal(err)
	}

	restarted, err := RestartIfAlreadyRunning(pidPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restarted {
		t.Fatal("expected no restart when pid file refers to current process")
	}
}

func TestReadPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pidPath := filepath.Join(dir, "curio.pid")
	if err := WritePIDFile(pidPath); err != nil {
		t.Fatal(err)
	}

	pid, err := ReadPID(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestIsGraceHandoff(t *testing.T) {
	t.Setenv(listenFdsKey, "")
	if IsGraceHandoff() {
		t.Fatal("expected no handoff without LISTEN_FDS")
	}

	t.Setenv(listenFdsKey, "2")
	if !IsGraceHandoff() {
		t.Fatal("expected handoff when LISTEN_FDS is set")
	}
}
