package helpers

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// WaitForTCP waits until something accepts TCP connections on addr (e.g. "127.0.0.1:4700").
// Use after starting an RPC server in a goroutine so clients do not dial before Listen completes.
func WaitForTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	dialAddr := loopbackDialAddr(addr)
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", dialAddr, 150*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, timeout, 25*time.Millisecond, "timeout waiting for TCP listener on %s", dialAddr)
}

// loopbackDialAddr maps a listen address to one we can dial from the same host.
func loopbackDialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("127.0.0.1", port)
	default:
		return addr
	}
}
