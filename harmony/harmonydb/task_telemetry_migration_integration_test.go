//go:build integration

package harmonydb

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgxpool"
)

// Frozen historical startup inputs, not fabricated base ledger entries. All
// selected files are the real production migrations through August 2026, plus
// the specified task telemetry variant. The independent MK20 release gate is
// not a prerequisite for task telemetry or its migration history.
//
//go:embed sql/202[3-5]*.sql sql/20260[1-8]*.sql sql/20260909-task-ownership-age.sql
var ownershipStartupFS embed.FS

//go:embed sql/202[3-5]*.sql sql/20260[1-8]*.sql sql/20260909-task-attempt-start.sql
var attemptStartupFS embed.FS

//go:embed sql/202[3-5]*.sql sql/20260[1-8]*.sql sql/20260909-task-attempt-start.sql sql/20260909-task-ownership-age.sql
var completeStartupFS embed.FS

//go:embed sql/202[3-5]*.sql sql/20260[1-8]*.sql sql/20260909-task-attempt-start.sql sql/20260909-task-ownership-age.sql sql/20260910-task-telemetry-reconcile.sql
var reconciledStartupFS embed.FS

type taskMigrationFixture struct {
	ctx    context.Context
	conn   *pgx.Conn
	cfg    Config
	schema string
}

func newTaskMigrationFixture(t *testing.T) *taskMigrationFixture {
	t.Helper()
	if os.Getenv("CURIO_TASK_MIGRATION_ITEST") != "1" {
		t.Skip("requires CURIO_TASK_MIGRATION_ITEST=1 and a dedicated disposable target")
	}
	// Reject ambient connection/read-only overrides before the first connection.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		for _, prefix := range []string{"PG", "HARMONYQUERY_", "CURIO_HARMONYDB_", "CURIO_DB_"} {
			require.False(t, strings.HasPrefix(key, prefix), "clear inherited variable %s", key)
		}
	}
	const prefix = "CURIO_TASK_MIGRATION_ITEST_"
	require.Equal(t, "127.0.0.1", os.Getenv(prefix+"HOST"), "literal IPv4 loopback is required")
	port, err := strconv.ParseUint(os.Getenv(prefix+"PORT"), 10, 16)
	require.NoError(t, err)
	require.NotZero(t, port)
	database, user := os.Getenv(prefix+"DATABASE"), os.Getenv(prefix+"USER")
	require.True(t, strings.HasPrefix(database, "curio_test_"), "dedicated DATABASE must start with curio_test_")
	require.NotEmpty(t, strings.TrimSpace(user))
	pcfg, err := pgx.ParseConfig("postgresql://placeholder@127.0.0.1/placeholder?sslmode=disable&load_balance=false")
	require.NoError(t, err)
	pcfg.Host, pcfg.Port, pcfg.Database, pcfg.User = "127.0.0.1", uint16(port), database, user
	pcfg.Password = os.Getenv(prefix + "PASSWORD")
	pcfg.Fallbacks, pcfg.ConnectTimeout = nil, 5*time.Second
	pcfg.RuntimeParams = map[string]string{"application_name": "curio-task-migration-itest"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	conn, err := pgx.ConnectConfig(ctx, pcfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		require.NoError(t, conn.Close(closeCtx))
	})
	// The real startup opens its own pool; require finite target-side limits too.
	for name, maximum := range map[string]int{"statement_timeout": 10000, "lock_timeout": 2000} {
		var limit int
		require.NoError(t, conn.QueryRow(ctx, "SELECT setting::int FROM pg_settings WHERE name=$1", name).Scan(&limit))
		require.True(t, limit > 0 && limit <= maximum, "%s must be bounded on the dedicated target", name)
	}
	var nonce [12]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	schema := "itest_task_migration_" + hex.EncodeToString(nonce[:])
	_, err = conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, err := conn.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, err)
	})
	var version, isolation string
	require.NoError(t, conn.QueryRow(ctx, "SELECT version()").Scan(&version))
	require.NoError(t, conn.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation))
	t.Logf("server=%s; requested isolation=driver default; reported=%s; Yugabyte effective isolation is not inferred from SHOW", version, isolation)
	return &taskMigrationFixture{ctx: ctx, conn: conn, schema: schema, cfg: Config{
		Hosts: []string{"127.0.0.1"}, Port: strconv.Itoa(int(port)), Database: database,
		Username: user, Password: pcfg.Password, Schema: schema, LoadBalance: false,
		ApplicationName: "curio-task-migration-itest",
		PoolConfig: &harmonyquery.PoolConfig{MaxConnections: 2, MinConnections: 0,
			MaxConnectionLifetime: time.Minute, MaxIdleTime: time.Second},
	}}
}

func (f *taskMigrationFixture) startup(t *testing.T, migrations *embed.FS) {
	t.Helper()
	cfg := f.cfg
	cfg.SqlEmbedFS = migrations // nil selects Curio's complete production embed.
	db, err := NewFromConfig(cfg)
	require.NoError(t, err)
	// HarmonyQuery's only public pool cleanup also drops this owned schema.
	// Cleanup runs after all assertions; repeated owned-schema drops are harmless.
	t.Cleanup(db.ITestDeleteAll)
}

func (f *taskMigrationFixture) seed(t *testing.T, attempt, ownership bool) {
	t.Helper()
	_, err := f.conn.Exec(f.ctx, `
		INSERT INTO harmony_machines (id,host_and_port,cpu,ram,gpu)
		VALUES (101,'worker-a.example:12300',4,1024,0),(102,'worker-b.example:12300',4,1024,0);
		INSERT INTO harmony_task (id,posted_time,update_time,owner_id,added_by,name,retries)
		VALUES (1,'2026-01-02 01:00:00+00','2026-01-02 02:00:00+00',101,101,'Synthetic',2),
		       (2,'2026-01-02 01:00:00+00','2026-01-02 02:00:00+00',NULL,101,'Synthetic',0),
		       (3,'2026-01-02 01:00:00+00','2026-01-02 02:00:00+00',101,101,'Synthetic',1)`)
	require.NoError(t, err)
	if attempt {
		_, err = f.conn.Exec(f.ctx, `UPDATE harmony_task SET attempt_id='valid-in-flight',
			attempt_started_at='2026-01-02 03:04:05.123456+00',attempt_start_source='do_entry' WHERE id=1;
			UPDATE harmony_task SET attempt_id='prepared-not-started',attempt_start_source='prepared' WHERE id=3`)
		require.NoError(t, err)
	}
	if ownership {
		_, err = f.conn.Exec(f.ctx, `UPDATE harmony_task SET work_start='2026-01-02 02:30:00.123456+00',work_start_source='claim' WHERE id IN (1,3)`)
		require.NoError(t, err)
	}
}

func (f *taskMigrationFixture) snapshot(t *testing.T, table string) []map[string]any {
	t.Helper()
	require.Contains(t, []string{"harmony_task", "base"}, table)
	rows, err := f.conn.Query(f.ctx, "SELECT to_jsonb(t) FROM "+pgx.Identifier{table}.Sanitize()+" t ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var raw []byte
		require.NoError(t, rows.Scan(&raw))
		var row map[string]any
		require.NoError(t, json.Unmarshal(raw, &row))
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

func (f *taskMigrationFixture) assertPreserved(t *testing.T, tasks, ledger []map[string]any) {
	t.Helper()
	// Exercise a real consumer-shaped SELECT: absence cannot hide in JSON output.
	_, err := f.conn.Exec(f.ctx, `SELECT owner_id,attempt_id,attempt_started_at,attempt_start_source,
		work_start,work_start_source,retries FROM harmony_task`)
	require.NoError(t, err, "both task telemetry schemas must be available after startup")
	after := f.snapshot(t, "harmony_task")
	require.Len(t, after, len(tasks))
	for i, before := range tasks {
		for key, value := range before {
			require.Equal(t, value, after[i][key], "task %v field %s changed during reconciliation", before["id"], key)
		}
		for _, key := range []string{"attempt_id", "attempt_started_at", "attempt_start_source", "work_start", "work_start_source"} {
			if _, existed := before[key]; !existed {
				require.Nil(t, after[i][key], "new %s must not be backfilled", key)
			}
		}
	}
	afterLedger := f.snapshot(t, "base")
	require.GreaterOrEqual(t, len(afterLedger), len(ledger))
	for i := range ledger {
		require.Equal(t, ledger[i], afterLedger[i], "historical ledger entry must not be edited/deleted")
	}
	var repaired, triggers int
	require.NoError(t, f.conn.QueryRow(f.ctx, "SELECT count(*) FROM base WHERE entry='20260910'").Scan(&repaired))
	require.Equal(t, 1, repaired, "real startup must reach and record the unique forward reconciliation")
	require.NoError(t, f.conn.QueryRow(f.ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='harmony_task'::regclass
		AND tgname IN ('harmony_task_clear_attempt_start_trigger','harmony_task_sync_work_start_trigger')`).Scan(&triggers))
	require.Equal(t, 2, triggers)
	var acquisitionLedger, acquisitionTrigger int
	require.NoError(t, f.conn.QueryRow(f.ctx, "SELECT count(*) FROM base WHERE entry='20260912'").Scan(&acquisitionLedger))
	require.Equal(t, 1, acquisitionLedger, "startup must reach the acquisition fence migration")
	require.NoError(t, f.conn.QueryRow(f.ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='harmony_task'::regclass AND tgname='harmony_task_acquisition_generation'`).Scan(&acquisitionTrigger))
	require.Equal(t, 1, acquisitionTrigger)
	for _, row := range after {
		require.NotNil(t, row["owner_generation"])
	}
	t.Logf("preserved %d task rows and %d original ledger rows; reconciliation recorded once", len(tasks), len(ledger))
}

func TestTaskTelemetryRunnerReconciliation(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		fs                 *embed.FS
		attempt, ownership bool
	}{
		{"fresh", nil, true, true},
		{"ownership_only", &ownershipStartupFS, false, true},
		{"attempt_only", &attemptStartupFS, true, false},
		{"fully_applied_in_flight", &completeStartupFS, true, true},
		{"previous_normal_schema", &reconciledStartupFS, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newTaskMigrationFixture(t)
			f.startup(t, scenario.fs)
			f.seed(t, scenario.attempt, scenario.ownership)
			tasks, ledger := f.snapshot(t, "harmony_task"), f.snapshot(t, "base")
			f.startup(t, nil)
			f.assertPreserved(t, tasks, ledger)
			stableTasks, stableLedger := f.snapshot(t, "harmony_task"), f.snapshot(t, "base")
			f.startup(t, nil)
			require.Equal(t, stableTasks, f.snapshot(t, "harmony_task"))
			require.Equal(t, stableLedger, f.snapshot(t, "base"), "restart must not manufacture another applied entry")
		})
	}
}

func TestTaskAcquisitionRunnerFailureRestart(t *testing.T) {
	f := newTaskMigrationFixture(t)
	f.startup(t, &reconciledStartupFS)
	f.seed(t, true, true)
	tasks, ledger := f.snapshot(t, "harmony_task"), f.snapshot(t, "base")
	locker, err := f.conn.Begin(f.ctx)
	require.NoError(t, err)
	_, err = locker.Exec(f.ctx, "LOCK TABLE harmony_task IN ACCESS SHARE MODE")
	require.NoError(t, err)
	var captured *pgxpool.Pool
	require.Nil(t, harmonyquery.ITestUpgradeFunc)
	harmonyquery.ITestUpgradeFunc = func(pool *pgxpool.Pool, _ string, _ string) { captured = pool }
	defer func() {
		harmonyquery.ITestUpgradeFunc = nil
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = locker.Rollback(c)
		if captured != nil {
			captured.Close()
		}
	}()
	_, err = NewFromConfig(f.cfg)
	require.ErrorContains(t, err, "20260912-task-acquisition-generation.sql")
	require.ErrorContains(t, err, "lock timeout")
	require.NoError(t, locker.Rollback(f.ctx))
	harmonyquery.ITestUpgradeFunc = nil
	if captured != nil {
		captured.Close()
		captured = nil
	}
	require.Equal(t, ledger, f.snapshot(t, "base"), "failed DDL must not fabricate its ledger entry")
	f.startup(t, nil)
	f.assertPreserved(t, tasks, ledger)
}

func TestTaskTelemetryRunnerPartialFailureRestart(t *testing.T) {
	f := newTaskMigrationFixture(t)
	require.Nil(t, harmonyquery.ITestUpgradeFunc, "migration observation hook must not already be in use")
	var locker pgx.Tx
	var captured *pgxpool.Pool
	defer func() {
		harmonyquery.ITestUpgradeFunc = nil
		if locker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = locker.Rollback(ctx)
		}
		if captured != nil {
			captured.Close()
		}
	}()
	// Pause the second DDL with a real competing relation lock. The first SQL
	// and its ledger insertion succeed normally; no fabricated history is used.
	harmonyquery.ITestUpgradeFunc = func(pool *pgxpool.Pool, _ string, sql string) {
		captured = pool
		if strings.Contains(sql, "FUNCTION harmony_task_clear_attempt_start()") {
			var err error
			locker, err = f.conn.Begin(f.ctx)
			require.NoError(t, err)
			_, err = locker.Exec(f.ctx, "LOCK TABLE harmony_task IN ACCESS SHARE MODE")
			require.NoError(t, err)
		}
	}
	cfg := f.cfg
	cfg.SqlEmbedFS = &completeStartupFS
	_, err := NewFromConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "20260909-task-ownership-age.sql")
	require.Contains(t, err.Error(), "lock timeout", "generic connection errors are not the injected DDL failure")
	require.NotNil(t, locker)
	require.NoError(t, locker.Rollback(f.ctx))
	locker = nil
	harmonyquery.ITestUpgradeFunc = nil
	captured.Close()
	captured = nil
	f.seed(t, true, false)
	tasks, ledger := f.snapshot(t, "harmony_task"), f.snapshot(t, "base")
	var oldCount int
	require.NoError(t, f.conn.QueryRow(f.ctx, "SELECT count(*) FROM base WHERE entry='20260909'").Scan(&oldCount))
	require.Equal(t, 1, oldCount)
	f.startup(t, nil)
	f.assertPreserved(t, tasks, ledger)
}
