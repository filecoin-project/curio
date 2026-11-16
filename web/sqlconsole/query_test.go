package sqlconsole

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestQueryACLLimitsRole(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}

	ctx := context.Background()
	res, err := Query(ctx, db, `SELECT 1`)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Reuse the same console pool for a second query.
	res2, err := Query(ctx, db, `SELECT current_user`)
	require.NoError(t, err)
	require.Len(t, res2.Rows, 1)
	require.Equal(t, consoleUser, res2.Rows[0][0])

	_, err = Query(ctx, db, `SELECT address FROM eth_keys`)
	require.Error(t, err)

	_, err = Query(ctx, db, `INSERT INTO harmony_task_history (task_id, name, posted, result) VALUES (0, 'x', NOW(), true)`)
	require.Error(t, err)

	// RESET/DEFAULT must not escalate: connection is authenticated as the console user.
	_, _ = Query(ctx, db, `
		DO $$ BEGIN
			EXECUTE 'SET LOCAL SESSION AUTHORIZATION DEFAULT';
		END $$`)
	_, err = Query(ctx, db, `SELECT address FROM eth_keys`)
	require.Error(t, err)
}
