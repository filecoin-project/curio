//go:build integration && !skiff

package harmonytask

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"
)

func assertCurrentTaskSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, column := range []struct{ table, name, kind string }{
		{"harmony_task", "update_time", "timestamp with time zone"},
		{"harmony_task", "posted_time", "timestamp with time zone"},
		{"harmony_task_history", "posted", "timestamp with time zone"},
		{"harmony_task_history", "work_start", "timestamp with time zone"},
		{"harmony_task_history", "work_end", "timestamp with time zone"},
		{"harmony_task", "retries", "bigint"},
		{"harmony_task", "owner_generation", "bigint"},
		{"harmony_task", "attempt_id", "text"},
		{"harmony_task", "attempt_started_at", "timestamp with time zone"},
		{"harmony_task", "work_start", "timestamp with time zone"},
	} {
		var kind string
		require.NoError(t, conn.QueryRow(ctx, `SELECT format_type(atttypid,atttypmod) FROM pg_attribute
 WHERE attrelid=$1::regclass AND attname=$2 AND NOT attisdropped`, column.table, column.name).Scan(&kind))
		require.Equal(t, column.kind, kind, "%s.%s", column.table, column.name)
		t.Logf("catalog %s.%s=%s", column.table, column.name, kind)
	}
	rows, err := conn.Query(ctx, `SELECT tgname,pg_get_triggerdef(oid) FROM pg_trigger WHERE tgrelid='harmony_task'::regclass AND NOT tgisinternal ORDER BY tgname`)
	require.NoError(t, err)
	triggers := map[string]string{}
	for rows.Next() {
		var name, definition string
		require.NoError(t, rows.Scan(&name, &definition))
		triggers[name] = definition
		t.Logf("catalog trigger %s: %s", name, definition)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	require.Contains(t, triggers, "harmony_task_clear_attempt_start_trigger")
	require.Contains(t, triggers, "harmony_task_sync_work_start_trigger")
	require.Contains(t, triggers, "harmony_task_acquisition_generation")
	var applied int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM base WHERE left(entry,8) IN ('20240522','20240927','20260909','20260910')`).Scan(&applied))
	require.GreaterOrEqual(t, applied, 4, "actual startup must reach reconciliation, not just a hand-picked SQL projection")
}
