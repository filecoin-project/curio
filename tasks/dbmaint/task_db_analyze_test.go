package dbmaint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func openITestDB(t *testing.T) *harmonydb.DB {
	t.Helper()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	if err == nil {
		return db
	}
	// Local Yugabyte often listens on 5433 instead of the default template Postgres:5432.
	if strings.Contains(err.Error(), "connection refused") {
		db, err = harmonydb.NewFromConfigWithITestID(t, harmonydb.YugabyteDB(true))
	}
	require.NoError(t, err)
	return db
}

func TestDoAnalyzesTables(t *testing.T) {
	ctx := t.Context()
	db := openITestDB(t)

	var schema string
	require.NoError(t, db.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema))
	counted, err := harmonydb.AdminTableCount(ctx, db, schema, "table_analyze_state")
	require.NoError(t, err, "COUNT(*) via AdminTableCount")
	require.GreaterOrEqual(t, counted, int64(0))

	// Fresh itest schema: no harmony_machine_details peers => upgrade guard is a no-op.
	// Empty table_analyze_state => shouldAnalyze returns true for every table (first sight).
	task := NewDBAnalyzeTask(db)
	done, err := task.Do(ctx, 1, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	var n int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM table_analyze_state`).Scan(&n))
	require.Greater(t, n, 0, "expected at least one table to be ANALYZEd and recorded")

	var sample string
	require.NoError(t, db.QueryRow(ctx, `
		SELECT table_name FROM table_analyze_state ORDER BY table_name LIMIT 1`).Scan(&sample))
	t.Logf("analyzed %d tables; sample=%s", n, sample)

	// Second pass with unchanged COUNT(*) should not bump analyze_count.
	var before int64
	require.NoError(t, db.QueryRow(ctx, `
		SELECT analyze_count FROM table_analyze_state WHERE table_name = $1`, sample).Scan(&before))

	done, err = task.Do(ctx, 2, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	var after int64
	require.NoError(t, db.QueryRow(ctx, `
		SELECT analyze_count FROM table_analyze_state WHERE table_name = $1`, sample).Scan(&after))
	require.Equal(t, before, after, "second pass should skip tables without 10%% row growth")
}

func TestShouldAnalyzeUsesRowCount(t *testing.T) {
	prev := int64(1000)
	require.True(t, shouldAnalyze(50, nil), "first sight")
	require.False(t, shouldAnalyze(1000, &prev), "unchanged")
	require.False(t, shouldAnalyze(1050, &prev), "delta below minRowDelta")
	require.True(t, shouldAnalyze(1100, &prev), "10% growth")
	require.True(t, shouldAnalyze(50, &prev), "shrink re-baselines")

	zero := int64(0)
	require.False(t, shouldAnalyze(0, &zero), "empty stays empty")
	require.True(t, shouldAnalyze(100, &zero), "growth off a zero baseline")
}

func TestDoSkipsWhenNewerMachineBooted(t *testing.T) {
	ctx := t.Context()
	db := openITestDB(t)

	var machineID int64
	require.NoError(t, db.QueryRow(ctx, `
		INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
		VALUES ('dbanalyze-upgrade-test', 1, 1, 0)
		RETURNING id`).Scan(&machineID))

	// Seed a peer that looks like a freshly booted higher version.
	_, err := db.Exec(ctx, `
		INSERT INTO harmony_machine_details (machine_id, version, startup_time)
		VALUES ($1, '999.0.0', NOW())`, machineID)
	require.NoError(t, err)

	task := NewDBAnalyzeTask(db)
	done, err := task.Do(ctx, 1, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	var n int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM table_analyze_state`).Scan(&n))
	require.Equal(t, 0, n, "upgrade quiet window should skip ANALYZE entirely")
}
