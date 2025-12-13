//go:build serial

package serial

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5/pgxpool"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonydb/testutil"
)

// TestSQLIdempotent tests that the SQL DDL files are idempotent.
// The upgrader will fail unless everything has "IF NOT EXISTS" or "IF EXISTS" statements.
// Or equivalent safety checks.
// NOTE: Cannot run in parallel - modifies global harmonydb.ITestUpgradeFunc
func TestSQLIdempotent(t *testing.T) {
	defer func() {
		harmonydb.ITestUpgradeFunc = nil
	}()

	// Use SetupTestDB to get a cloned schema quickly (all structures already exist)
	testID := testutil.SetupTestDB(t)
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)

	// Clear migration tracking so migrations will re-run
	ctx := context.Background()
	_, err = cdb.Exec(ctx, `DELETE FROM base`)
	require.NoError(t, err)

	// Set up idempotency check - each migration SQL will be run twice
	harmonydb.ITestUpgradeFunc = func(pool *pgxpool.Pool, name string, sql string) {
		_, err := pool.Exec(context.Background(), sql)
		if err != nil {
			require.NoError(t, fmt.Errorf("SQL DDL file failed idempotent check: %s, %w", name, err))
		}
	}

	// Create second connection - migrations re-run on existing structures (tests idempotency)
	// Keep both connections open - cleanup handles closing
	_, err = harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)

	_, err = cdb.Exec(ctx, `
			INSERT INTO 
				itest_scratch (content, some_int) 
				VALUES 
				('andy was here', 5), 
				('lotus is awesome', 6)
			`)
	require.NoError(t, err)
}
