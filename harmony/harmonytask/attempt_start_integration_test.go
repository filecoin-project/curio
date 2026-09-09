//go:build integration && !skiff

package harmonytask

import (
	"context"
	"database/sql"
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

// No regular Curio/libpq defaults are used. Each invocation owns one fresh
// HarmonyDB test namespace and only drops that namespace during cleanup.
func attemptSQLFixture(t *testing.T) (context.Context, *harmonydb.DB, *harmonydb.DB, *pgx.Conn) {
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
	cfg.ReadOnly, cfg.LoadBalance = true, false
	cfg.ApplicationName = "curio-attempt-itest"
	first, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(first.ITestDeleteAll)
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
	applyAttemptMigration(t, ctx, conn, "20230719-harmony.sql")
	_, err = first.Exec(ctx, `INSERT INTO harmony_machines (id,host_and_port,cpu,ram,gpu) VALUES (101,'worker-a.example:12300',8,1024,0),(102,'worker-b.example:12300',8,1024,0)`)
	require.NoError(t, err)
	var version, isolation string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	require.NoError(t, conn.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation))
	t.Logf("database=%s requested isolation=driver default; SQL transaction_isolation=%s; Yugabyte effective isolation=UNVERIFIED", version, isolation)
	return ctx, first, second, conn
}

func applyAttemptMigration(t *testing.T, ctx context.Context, conn *pgx.Conn, name string) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	contents, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "harmonydb", "sql", name))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(contents))
	require.NoError(t, err)
}

func TestTaskAttemptSQLMigrationAndIdentity(t *testing.T) {
	ctx, db, other, conn := attemptSQLFixture(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task (id,posted_time,owner_id,added_by,name) VALUES (1,CURRENT_TIMESTAMP-INTERVAL '3 hours',101,101,'Synthetic'),(2,CURRENT_TIMESTAMP,NULL,101,'Synthetic')`)
	require.NoError(t, err)
	applyAttemptMigration(t, ctx, conn, "20260909-task-ownership-age.sql")
	applyAttemptMigration(t, ctx, conn, "20260909-task-attempt-start.sql")
	var start sql.NullTime
	var token, source sql.NullString
	read := func(id int) {
		t.Helper()
		require.NoError(t, db.QueryRow(ctx, `SELECT attempt_started_at,attempt_id,attempt_start_source FROM harmony_task WHERE id=$1`, id).Scan(&start, &token, &source))
	}
	read(1)
	require.False(t, start.Valid || token.Valid || source.Valid, "upgrade must not backfill execution provenance")
	first := harmonyTaskAttemptStore{db: db, owner: 101}
	delayed := harmonyTaskAttemptStore{db: other, owner: 101}
	ids, tokens := prepareTaskAttempts(ctx, first, []TaskID{1})
	require.Equal(t, []TaskID{1}, ids)
	old := tokens[1]
	entry := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ok, err := first.record(ctx, 1, old, entry)
	require.NoError(t, err)
	require.True(t, ok)
	read(1)
	require.True(t, start.Time.Equal(entry))
	require.Equal(t, "do_entry", source.String)
	// Reapplying the exact migration models the existing personal schema upgrade.
	applyAttemptMigration(t, ctx, conn, "20260909-task-attempt-start.sql")
	read(1)
	require.True(t, start.Time.Equal(entry))
	var triggerCount int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='harmony_task'::regclass AND tgname='harmony_task_clear_attempt_start_trigger'`).Scan(&triggerCount))
	require.Equal(t, 1, triggerCount)
	_, next := prepareTaskAttempts(ctx, first, []TaskID{1})
	require.NotEqual(t, old, next[1], "same-owner recovery must replace attempt identity")
	ok, err = delayed.record(ctx, 1, old, entry)
	require.NoError(t, err)
	require.False(t, ok, "independent delayed writer must not populate the next attempt")
	read(1)
	require.False(t, start.Valid)
	require.Equal(t, "prepared", source.String)
	ok, err = first.record(ctx, 1, next[1], entry.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = delayed.record(ctx, 1, next[1], entry)
	require.NoError(t, err)
	require.False(t, ok, "already populated starts are immutable")
	_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=102 WHERE id=1`)
	require.NoError(t, err)
	read(1)
	require.False(t, start.Valid || token.Valid)
	require.Equal(t, "claimed", source.String)
	require.Error(t, first.prepare(ctx, 1, "stale-owner"))
	ok, err = delayed.record(ctx, 1, next[1], entry)
	require.NoError(t, err)
	require.False(t, ok)
	read(2)
	require.False(t, start.Valid || token.Valid, "unrelated pending row changed")
	_, err = db.Exec(ctx, `INSERT INTO harmony_task (id,posted_time,owner_id,added_by,name,attempt_id,attempt_started_at,attempt_start_source) VALUES (3,CURRENT_TIMESTAMP,101,101,'Synthetic','invented',CURRENT_TIMESTAMP,'do_entry')`)
	require.NoError(t, err)
	read(3)
	require.False(t, start.Valid || token.Valid, "fresh inserts cannot invent an executed attempt")
	require.Equal(t, "claimed", source.String)
	_, err = db.Exec(ctx, `DELETE FROM harmony_machines WHERE id=102`)
	require.NoError(t, err)
	read(1)
	require.False(t, start.Valid || token.Valid, "FK ownership release must clear telemetry")
}

func TestTaskAttemptSQLDoEntryAndRollback(t *testing.T) {
	ctx, db, _, conn := attemptSQLFixture(t)
	applyAttemptMigration(t, ctx, conn, "20260909-task-ownership-age.sql")
	applyAttemptMigration(t, ctx, conn, "20260909-task-attempt-start.sql")
	_, err := db.Exec(ctx, `INSERT INTO harmony_task (id,posted_time,owner_id,added_by,name) VALUES (1,CURRENT_TIMESTAMP-INTERVAL '3 hours',101,101,'Synthetic')`)
	require.NoError(t, err)
	store := harmonyTaskAttemptStore{db: db, owner: 101}
	_, tokens := prepareTaskAttempts(ctx, store, []TaskID{1})
	entry := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var history time.Time
	done, err := runWithAttemptStart(ctx, store, 1, tokens[1], nil, func() time.Time { return entry }, func(start time.Time) { history = start }, func() (bool, error) {
		// Observe the actual asynchronous adapter write before completing the
		// synthetic body. This is bounded polling, not contention evidence.
		for {
			var recorded sql.NullTime
			if err := db.QueryRow(ctx, `SELECT attempt_started_at FROM harmony_task WHERE id=1`).Scan(&recorded); err != nil {
				return false, err
			}
			if recorded.Valid {
				return recorded.Time.Equal(entry), nil
			}
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
	})
	require.NoError(t, err)
	require.True(t, done)
	require.True(t, history.Equal(entry), "live and new History starts must share the same instant")
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(PREPARE_TASK_ATTEMPT, "rolled-back", 1, 101)
		return false, err
	})
	require.NoError(t, err)
	require.False(t, committed)
	var stored string
	require.NoError(t, db.QueryRow(ctx, `SELECT attempt_id FROM harmony_task WHERE id=1`).Scan(&stored))
	require.Equal(t, tokens[1], stored)
}
