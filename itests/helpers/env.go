package helpers

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func EnvElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func FreeListenAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}
