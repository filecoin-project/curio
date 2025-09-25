package itests

import (
	"context"
	"fmt"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5/pgxpool"
)

// TestSQLIdempotent tests that the SQL DDL files are idempotent.
// The upgrader will fail unless everything has "IF NOT EXISTS" or "IF EXISTS" statements.
// Or equivalent safety checks.
func TestSQLIdempotent(t *testing.T) {
	harmonydb.ITestUpgradeFunc = func(db *pgxpool.Pool, name string, sql string) {
		_, err := db.Exec(context.Background(), sql)
		require.NoError(t, fmt.Errorf("SQL DDL file failed idempotent check: %s, %w", name, err))
	}

	testID := harmonydb.ITestNewID()
	cdb, err := harmonydb.NewFromConfigWithITestID(t, testID)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = cdb.Exec(ctx, `
			INSERT INTO 
				itest_scratch (content, some_int) 
				VALUES 
				('andy was here', 5), 
				('lotus is awesome', 6)
			`)
	require.NoError(t, err)
}
