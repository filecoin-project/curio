//go:build integration && !skiff

package harmonytask

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// No regular Curio/libpq defaults are used. Each invocation owns one fresh
// HarmonyDB test namespace and only drops that namespace during cleanup.
func retrySQLFixture(t *testing.T, capture ...func(harmonydb.Config)) (context.Context, *harmonydb.DB, *harmonydb.DB, *pgx.Conn) {
	t.Helper()
	if os.Getenv("CURIO_TASK_ATTEMPT_ITEST") != "1" {
		t.Skip("requires explicit CURIO_TASK_ATTEMPT_ITEST=1 and a disposable loopback target")
	}
	const prefix = "CURIO_TASK_ATTEMPT_ITEST_"
	host, port := os.Getenv(prefix+"HOST"), os.Getenv(prefix+"PORT")
	address := net.ParseIP(host)
	require.True(t, address != nil && address.IsLoopback(), "HOST must be a literal loopback IP")
	n, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	require.NotZero(t, n)
	database, user := os.Getenv(prefix+"DATABASE"), os.Getenv(prefix+"USER")
	require.NotEmpty(t, strings.TrimSpace(database))
	require.NotEmpty(t, strings.TrimSpace(user))
	opts := harmonydb.ItestOptions{Hosts: []string{host}, Port: port, Database: database,
		Username: user, Password: os.Getenv(prefix + "PASSWORD"), ITestID: harmonydb.ITestNewID()}
	cfg := opts.HarmonyConfig()
	cfg.ReadOnly, cfg.LoadBalance = false, false
	cfg.ApplicationName = "curio-attempt-itest"
	first, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(first.ITestDeleteAll)
	cfg.ReadOnly = true
	for _, f := range capture {
		f(cfg)
	}
	second, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(second.ITestDeleteAll)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pcfg, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false")
	require.NoError(t, err)
	pcfg.Host, pcfg.Port, pcfg.Database, pcfg.User, pcfg.Password = host, uint16(n), database, user, opts.Password
	pcfg.Fallbacks, pcfg.ConnectTimeout = nil, 5*time.Second
	pcfg.RuntimeParams = map[string]string{"search_path": "itest_" + string(opts.ITestID), "statement_timeout": "5000", "lock_timeout": "2000"}
	conn, err := pgx.ConnectConfig(ctx, pcfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, conn.Close(closeCtx))
	})
	for _, column := range []struct{ table, name, kind string }{
		{"harmony_task", "update_time", "timestamp with time zone"},
		{"harmony_task", "posted_time", "timestamp with time zone"},
		{"harmony_task", "retries", "bigint"},
		{"harmony_task_history", "work_start", "timestamp with time zone"},
	} {
		var kind string
		require.NoError(t, conn.QueryRow(ctx, "SELECT format_type(atttypid,atttypmod) FROM pg_attribute WHERE attrelid=$1::regclass AND attname=$2 AND NOT attisdropped", column.table, column.name).Scan(&kind))
		require.Equal(t, column.kind, kind)
	}
	var applied int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM base WHERE left(entry,8) IN ('20240522','20240927')").Scan(&applied))
	require.Equal(t, 2, applied, "embedded startup runner must reach timestamp and retry migrations")
	_, err = first.Exec(ctx, `INSERT INTO harmony_machines (id,host_and_port,cpu,ram,gpu) VALUES (101,'worker-a.example:12300',8,1024,0),(102,'worker-b.example:12300',8,1024,0)`)
	require.NoError(t, err)
	var version, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))
	t.Logf("database=%s requested isolation=driver default; SQL transaction_isolation=%s; Yugabyte effective isolation=UNVERIFIED", version, isolation)
	return ctx, first, second, conn
}
