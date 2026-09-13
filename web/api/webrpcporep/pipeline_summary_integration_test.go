//go:build integration && !skiff

package webrpcporep

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

func summarySQLFixture(t *testing.T) (context.Context, *harmonydb.DB, *pgx.Conn) {
	t.Helper()
	ctx, db, conn := porepSQLFixture(t) // Reuse its explicit opt-in/loopback/owned-schema guard.
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	for _, name := range []string{"20240522-ts-to-timestampz.sql", "20260910-task-telemetry-reconcile.sql", "20260912-task-acquisition-generation.sql"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "..", "harmony", "harmonydb", "sql", name))
		require.NoError(t, err)
		sql := string(b)
		if name == "20240522-ts-to-timestampz.sql" {
			var found bool
			sql, _, found = strings.Cut(sql, "-- Convert timestamps in sector_location table")
			require.True(t, found) // Only the real Harmony timestamp conversions are needed.
		}
		_, err = conn.Exec(ctx, sql)
		require.NoError(t, err)
	}
	var schema, poolSchema string
	require.NoError(t, conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema))
	require.NoError(t, db.QueryRow(ctx, `SELECT current_schema()`).Scan(&poolSchema))
	require.True(t, strings.HasPrefix(schema, "itest_"))
	require.Equal(t, schema, poolSchema)
	t.Logf("owned schema=%s; real PKs, task-owner FK and telemetry/acquisition triggers; no startup/native/chain execution", schema)
	return ctx, db, conn
}

func assertSummaryReconciles(t *testing.T, rows []PorepPipelineSummary) {
	t.Helper()
	for _, r := range rows {
		c := r.SectorCounts
		require.NotNil(t, c)
		require.False(t, c.ObservedAt.IsZero())
		require.Equal(t, rows[0].SectorCounts.ObservedAt, c.ObservedAt)
		require.Equal(t, c.Total, c.Remaining+c.Complete)
		require.Equal(t, int64(r.CountSDR), c.SDRTotal)
		require.Equal(t, c.SDRTotal, c.SDRRunning+c.SDRPreparing+c.SDRWaitingTask+c.SDRWaitingCreate+c.SDRMissingTask+c.SDROtherTask+c.SDRFailed+c.SDRUnknown)
		require.Equal(t, c.Remaining, c.SDRTotal+c.PostSDR+c.Failed-c.SDRFailed)
	}
}

func TestPoRepSummarySQLClassification(t *testing.T) {
	ctx, db, conn := summarySQLFixture(t)
	chain := &summaryChain{}
	a := New(&webrpc.Handler{Deps: &deps.Deps{Chain: chain, DB: db}})
	load := func() []PorepPipelineSummary {
		t.Helper()
		rows, err := a.PorepPipelineSummary(ctx) // Actual handler, query and mapper.
		require.NoError(t, err)
		assertSummaryReconciles(t, rows)
		return rows
	}
	require.Empty(t, load())
	_, err := conn.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu)
 VALUES(1,'fixture-one.invalid:1',8,1024,0),(2,'fixture-two.invalid:2',8,1024,0);
 INSERT INTO harmony_task(id,posted_time,owner_id,added_by,name) VALUES(1,now()-INTERVAL '3 hours',NULL,1,'SDR');
 INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof) VALUES(1000,0,8);`)
	require.NoError(t, err)
	c := load()[0].SectorCounts
	require.Equal(t, int64(1), c.SDRWaitingCreate)
	// State transitions use the real ownership/attempt-clearing triggers. Fixture
	// Do-entry writes model the runner's private prepared->do_entry statements;
	// they do not execute a scheduler or native body.
	steps := []struct{ name, sql, field string }{
		{"queued", `UPDATE sectors_sdr_pipeline SET task_id_sdr=1`, "SDRWaitingTask"},
		{"claimed-not-entered", `UPDATE harmony_task SET owner_id=1 WHERE id=1`, "SDRPreparing"},
		{"prepared", `UPDATE harmony_task SET attempt_id='attempt-one',attempt_start_source='prepared' WHERE id=1`, "SDRPreparing"},
		{"entered-not-posted-age", `UPDATE harmony_task SET attempt_started_at=statement_timestamp(),attempt_start_source='do_entry' WHERE id=1`, "SDRRunning"},
		{"failure-priority", `UPDATE sectors_sdr_pipeline SET failed=true`, "SDRFailed"},
		{"retry-unowned", `UPDATE sectors_sdr_pipeline SET failed=false; UPDATE harmony_task SET owner_id=NULL WHERE id=1`, "SDRWaitingTask"},
		{"unowned-conflicting-attempt", `UPDATE harmony_task SET attempt_id='stale' WHERE id=1`, "SDRUnknown"},
		{"new-owner", `UPDATE harmony_task SET owner_id=2 WHERE id=1`, "SDRPreparing"},
		{"new-attempt", `UPDATE harmony_task SET attempt_id='attempt-two',attempt_started_at=statement_timestamp(),attempt_start_source='do_entry' WHERE id=1`, "SDRRunning"},
		{"same-owner-recovery", `UPDATE harmony_task SET owner_generation=owner_generation+1 WHERE id=1`, "SDRPreparing"},
		{"missing-token", `UPDATE harmony_task SET attempt_start_source='do_entry',attempt_started_at=statement_timestamp() WHERE id=1`, "SDRUnknown"},
		{"old-attempt", `UPDATE harmony_task SET attempt_id='old',attempt_started_at=work_start-INTERVAL '1 second' WHERE id=1`, "SDRUnknown"},
		{"future-attempt", `UPDATE harmony_task SET attempt_started_at=now()+INTERVAL '1 hour' WHERE id=1`, "SDRUnknown"},
		{"backfill-unknown", `UPDATE harmony_task SET attempt_started_at=statement_timestamp(),work_start_source=NULL WHERE id=1`, "SDRUnknown"},
		{"stale-owner", `UPDATE harmony_task SET work_start_source='claim'; UPDATE harmony_machines SET last_contact=now()-INTERVAL '3 minutes' WHERE id=2`, "SDRUnknown"},
		{"future-heartbeat", `UPDATE harmony_machines SET last_contact=now()+INTERVAL '1 hour' WHERE id=2`, "SDRUnknown"},
		{"fresh-owner", `UPDATE harmony_machines SET last_contact=statement_timestamp() WHERE id=2`, "SDRRunning"},
		{"conflicting-later-stage", `UPDATE sectors_sdr_pipeline SET after_porep=true`, "SDRUnknown"},
		{"restored-stage", `UPDATE sectors_sdr_pipeline SET after_porep=false`, "SDRRunning"},
		{"key-regen-excluded", `UPDATE harmony_task SET name='SDRKeyRegen' WHERE id=1`, "SDROtherTask"},
		{"batch-not-standard-sdr", `UPDATE harmony_task SET name='Batch8-32G' WHERE id=1`, "SDROtherTask"},
		{"wrong-kind", `UPDATE harmony_task SET name='PoRep' WHERE id=1`, "SDROtherTask"},
		{"broken-reference", `DELETE FROM harmony_task WHERE id=1`, "SDRMissingTask"},
		{"failed-broken-reference-priority", `UPDATE sectors_sdr_pipeline SET failed=true`, "SDRFailed"},
		{"sdr-complete", `UPDATE sectors_sdr_pipeline SET after_sdr=true,failed=false`, "PostSDR"},
		{"post-sdr-failed", `UPDATE sectors_sdr_pipeline SET failed=true`, "Failed"},
		{"commit-confirmed-finalize-pending", `UPDATE sectors_sdr_pipeline SET failed=false,after_porep=true,after_commit_msg_success=true`, "PostSDR"},
		{"finalize-done-storage-pending", `UPDATE sectors_sdr_pipeline SET after_finalize=true`, "PostSDR"},
		{"storage-done", `UPDATE sectors_sdr_pipeline SET after_move_storage=true`, "Complete"},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			_, err := conn.Exec(ctx, step.sql)
			require.NoError(t, err)
			rows := load()
			wire, err := json.Marshal(rows[0].SectorCounts)
			require.NoError(t, err)
			var fields map[string]interface{}
			require.NoError(t, json.Unmarshal(wire, &fields))
			require.Equal(t, float64(1), fields[step.field], "%s: %s", step.name, wire)
			if step.field != "SDRRunning" {
				require.Zero(t, rows[0].SectorCounts.SDRRunning)
			}
		})
	}
	require.Equal(t, len(steps)+2, chain.calls)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	rows, err := loadPoRepSummary(cancelled, 0, db)
	require.Error(t, err)
	require.Nil(t, rows)
}

func TestPoRepSummarySQLLargeSnapshot(t *testing.T) {
	ctx, db, conn := summarySQLFixture(t)
	_, err := conn.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu) VALUES(1,'fixture.invalid:1',8,1024,0);
 INSERT INTO harmony_task(id,posted_time,owner_id,added_by,name) VALUES(1,now(),1,1,'SDR'),(2,now(),NULL,1,'SDR');
 UPDATE harmony_task SET attempt_id='live',attempt_start_source='do_entry',attempt_started_at=statement_timestamp() WHERE id=1;
 INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof) SELECT 1000+(n%2),n,8 FROM generate_series(1,32108)n;
 INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,task_id_sdr) VALUES(1000,50000,8,1),(1001,50000,8,2),(1001,50001,8,999);
 INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,after_sdr,failed,after_commit_msg_success,after_finalize,after_move_storage)
 VALUES(1000,50002,8,true,false,true,false,false),(1000,50003,8,true,false,true,true,true),(1001,50004,8,true,true,false,false,false);
 ANALYZE sectors_sdr_pipeline; ANALYZE harmony_task; ANALYZE harmony_machines;`)
	require.NoError(t, err)
	start := time.Now()
	rows, err := loadPoRepSummary(ctx, 0, db)
	require.NoError(t, err)
	assertSummaryReconciles(t, rows)
	require.Len(t, rows, 2)
	one, two := rows[0].SectorCounts, rows[1].SectorCounts
	require.Equal(t, int64(16057), one.Total)
	require.Equal(t, int64(16057), two.Total)
	require.Equal(t, int64(16054), one.SDRWaitingCreate)
	require.Equal(t, int64(16054), two.SDRWaitingCreate)
	require.Equal(t, int64(1), one.SDRRunning)
	require.Zero(t, two.SDRRunning)
	require.Equal(t, int64(1), two.SDRWaitingTask)
	require.Equal(t, int64(1), two.SDRMissingTask)
	require.Equal(t, int64(1), one.PostSDR)
	require.Equal(t, int64(1), one.Complete)
	require.Equal(t, int64(1), two.Failed)
	wire, err := json.Marshal(rows)
	require.NoError(t, err)
	require.Less(t, len(wire), 4096)
	t.Logf("one statement; 32108 pending + 6 other sectors; returned=%d bytes=%d wall=%s; retries=NOT_MEASURED", len(rows), len(wire), time.Since(start))
	plan, err := conn.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+porepSummaryQuery, 0)
	require.NoError(t, err)
	for plan.Next() {
		var line string
		require.NoError(t, plan.Scan(&line))
		t.Log(line)
	}
	require.NoError(t, plan.Err())
	plan.Close()
	// Actual read failure must not return a partial/empty successful aggregate.
	_, err = conn.Exec(ctx, `ALTER TABLE harmony_task RENAME COLUMN attempt_id TO fixture_missing_attempt`)
	require.NoError(t, err)
	broken, err := loadPoRepSummary(ctx, 0, db)
	require.Error(t, err)
	require.Nil(t, broken, fmt.Sprint(err))
}
