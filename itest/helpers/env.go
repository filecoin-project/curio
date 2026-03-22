package helpers

import (
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func EnvElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

// IndexstoreHost returns the host for Cassandra/Scylla indexstore connections.
// Uses CURIO_DB_HOST_CQL if set (e.g. when Postgres and Scylla run in separate containers),
// otherwise falls back to CURIO_HARMONYDB_HOSTS (e.g. when using YugabyteDB with both in one).
func IndexstoreHost() string {
	return EnvElse("CURIO_DB_HOST_CQL", EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1"))
}

// IndexstorePort returns the CQL port for Cassandra/Scylla indexstore connections.
// Uses CURIO_HARMONYDB_CQL_PORT if set (e.g. when started via testcontainers with
// dynamic port mapping), otherwise defaults to 9042.
func IndexstorePort() int {
	if v := os.Getenv("CURIO_HARMONYDB_CQL_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			return p
		}
	}
	return 9042
}

func FreeListenAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}
