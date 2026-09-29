package harmonydb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Static compatibility checks, not SQL execution evidence.
func TestTaskTelemetryMigrationDates(t *testing.T) {
	files, err := upgradeFS.ReadDir("sql")
	require.NoError(t, err)
	dates := make(map[string][]string)
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") {
			require.GreaterOrEqual(t, len(file.Name()), 8)
			if file.Name()[:8] >= "20260909" {
				dates[file.Name()[:8]] = append(dates[file.Name()[:8]], file.Name())
			}
		}
	}
	for date, names := range dates {
		if date == "20260909" {
			require.ElementsMatch(t, []string{"20260909-task-attempt-start.sql", "20260909-task-ownership-age.sql"}, names,
				"only the preserved historical collision is allowed")
			continue
		}
		require.Len(t, names, 1, "new migration date %s must be unique for the pinned runner", date)
	}
}

func TestTaskTelemetryReconciliationKeepsTriggerSemantics(t *testing.T) {
	repair, err := upgradeFS.ReadFile("sql/20260910-task-telemetry-reconcile.sql")
	require.NoError(t, err)
	for _, name := range []string{"20260909-task-attempt-start.sql", "20260909-task-ownership-age.sql"} {
		original, err := upgradeFS.ReadFile("sql/" + name)
		require.NoError(t, err)
		_, definitions, ok := strings.Cut(string(original), "CREATE OR REPLACE FUNCTION ")
		require.True(t, ok)
		// Ignore only trailing explanatory comments, not any SQL statement.
		end := strings.LastIndex(definitions, ";")
		require.NotEqual(t, -1, end)
		require.Contains(t, string(repair), "CREATE OR REPLACE FUNCTION "+definitions[:end+1],
			"reconciliation must preserve the existing function and trigger definitions from %s", name)
	}
}
