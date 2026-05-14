package helpers

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/market/indexstore"
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

// YBCQLPort returns the YCQL port for test connections. It reads
// CURIO_HARMONYDB_CQL_PORT (set by testcontainers with dynamic port mapping)
// and falls back to the default 9042.
func YBCQLPort() int {
	if v := os.Getenv("CURIO_HARMONYDB_CQL_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err == nil {
			return p
		}
	}
	return 9042
}

func NewIndexStore(ctx context.Context, t *testing.T, cfg *config.CurioConfig) *indexstore.IndexStore {
	hosts := []string{EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}
	idxStore, err := indexstore.NewIndexStore(hosts, YBCQLPort(), cfg)
	require.NoError(t, err)
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)
	return idxStore
}
