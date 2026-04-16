package helpers

import (
	"context"
	"net"
	"os"
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

func NewIndexStore(ctx context.Context, t *testing.T, cfg *config.CurioConfig) *indexstore.IndexStore {
	hosts := []string{EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}
	idxStore, err := indexstore.NewIndexStore(hosts, 9042, cfg)
	require.NoError(t, err)
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)
	return idxStore
}
