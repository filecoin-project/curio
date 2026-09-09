//go:build integration && !skiff

package webrpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestClusterTaskSnapshotSQL(t *testing.T) {
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
	opts := harmonydb.ItestOptions{Hosts: []string{host}, Port: port, Database: database, Username: user,
		Password: os.Getenv(prefix + "PASSWORD"), ITestID: harmonydb.ITestNewID()}
	cfg := opts.HarmonyConfig()
	cfg.ReadOnly, cfg.LoadBalance = true, false
	db, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(db.ITestDeleteAll)
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
	_, path, _, ok := runtime.Caller(0)
	require.True(t, ok)
	for _, name := range []string{"20230719-harmony.sql", "20260909-task-ownership-age.sql", "20260909-task-attempt-start.sql"} {
		contents, err := os.ReadFile(filepath.Join(filepath.Dir(path), "..", "..", "..", "harmony", "harmonydb", "sql", name))
		require.NoError(t, err)
		_, err = conn.Exec(ctx, string(contents))
		require.NoError(t, err)
	}
	_, err = db.Exec(ctx, `INSERT INTO harmony_machines (id,host_and_port,cpu,ram,gpu) VALUES (101,'worker.example:12300',8,1024,0)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_task (id,posted_time,owner_id,added_by,name)
SELECT n, CURRENT_TIMESTAMP-INTERVAL '3 hours', CASE WHEN n<=3 THEN 101 ELSE NULL END,101,'Synthetic' FROM generate_series(1,503) n`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE harmony_task SET attempt_id='current',attempt_started_at=CURRENT_TIMESTAMP-INTERVAL '10 minutes',attempt_start_source='do_entry' WHERE id=1`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE harmony_task SET attempt_start_source=NULL WHERE id=3`)
	require.NoError(t, err)
	source := harmonyClusterTaskSummarySource{db: db}
	applied := ClusterTaskSummaryApplied{MaxTasks: 5, MaxPending: 2}
	started := time.Now()
	snapshot, err := source.LoadSnapshot(ctx, applied)
	require.NoError(t, err)
	t.Logf("bounded snapshot wall=%s returned=%d running_total=%d pending_total=%d; row limit is not a scanned-row bound", time.Since(started), len(snapshot.Rows), snapshot.RunningTotal, snapshot.PendingTotal)
	require.Len(t, snapshot.Rows, 5)
	require.Equal(t, int64(3), snapshot.RunningTotal)
	require.Equal(t, int64(500), snapshot.PendingTotal)
	for i, expected := range []string{"running", "awaiting-start", "unknown", "pending", "pending"} {
		row := buildLimitedTaskSummary(snapshot.Rows[i], snapshot.ObservedAt, nil)
		require.Equal(t, expected, row.TookState)
		if i == 0 {
			require.NotNil(t, row.TookSeconds)
			require.InDelta(t, 600, *row.TookSeconds, 3)
		} else {
			require.Nil(t, row.TookSeconds)
		}
	}
	types, err := source.LoadTaskTypes(ctx, false)
	require.NoError(t, err)
	require.Len(t, types, 1)
	rows, err := conn.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+clusterTaskSummaryLimitedQuery, clusterTaskSummaryLimitedQueryArgs(applied)...)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		t.Log(line)
	}
	require.NoError(t, rows.Err())
}
