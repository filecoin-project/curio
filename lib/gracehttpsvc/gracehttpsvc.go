// Package gracehttpsvc integrates facebookgo/gracehttp for zero-downtime
// HTTP restarts via SIGUSR2 and graceful shutdown via SIGTERM.
package gracehttpsvc

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/facebookgo/grace/gracehttp"
)

var preRestartHook func() error

// SetPreRestartHook registers a callback invoked immediately before a graceful
// restart forks the successor process. Use it to drain in-flight work.
func SetPreRestartHook(hook func() error) {
	preRestartHook = hook
}

// TriggerRestart requests a zero-downtime restart by sending SIGUSR2 to the
// current process. gracehttp will fork a successor and hand off listeners.
func TriggerRestart() error {
	return syscall.Kill(os.Getpid(), syscall.SIGUSR2)
}

// TriggerShutdown requests a graceful shutdown by sending SIGTERM to the
// current process. gracehttp will drain HTTP connections before exiting.
func TriggerShutdown() error {
	return syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

// RestartPID sends SIGUSR2 to the process identified by pid.
func RestartPID(pid int) error {
	return syscall.Kill(pid, syscall.SIGUSR2)
}

// WritePIDFile records the current process id at path.
func WritePIDFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating pid file directory: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
}

// RemovePIDFile deletes the pid file at path.
func RemovePIDFile(path string) {
	_ = os.Remove(path)
}

// RestartFromPIDFile reads a pid from path and sends SIGUSR2 to that process.
func RestartFromPIDFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading pid file %s: %w", path, err)
	}
	pid, err := strconv.Atoi(string(data[:len(data)-1]))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		pid, err = strconv.Atoi(string(data))
	}
	if err != nil {
		return fmt.Errorf("parsing pid from %s: %w", path, err)
	}
	if err := RestartPID(pid); err != nil {
		return fmt.Errorf("sending SIGUSR2 to pid %d: %w", pid, err)
	}
	return nil
}

// Serve starts one or more HTTP servers with gracehttp, enabling graceful
// shutdown on SIGTERM/SIGINT and zero-downtime restart on SIGUSR2.
func Serve(servers ...*http.Server) error {
	if len(servers) == 0 {
		return fmt.Errorf("no http servers to serve")
	}

	if preRestartHook != nil {
		return gracehttp.ServeWithOptions(servers, gracehttp.PreStartProcess(preRestartHook))
	}
	return gracehttp.Serve(servers...)
}
