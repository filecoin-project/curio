package harmonydb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminAnalyzeRequiresSchemaAndTable(t *testing.T) {
	ctx := context.Background()
	require.Error(t, AdminAnalyze(ctx, nil, "public", "t"))
	require.Error(t, AdminAnalyze(ctx, &DB{}, "", "t"))
	require.Error(t, AdminAnalyze(ctx, &DB{}, "public", ""))
}

func TestAdminTableCountRequiresSchemaAndTable(t *testing.T) {
	ctx := context.Background()
	_, err := AdminTableCount(ctx, nil, "public", "t")
	require.Error(t, err)
	_, err = AdminTableCount(ctx, &DB{}, "", "t")
	require.Error(t, err)
	_, err = AdminTableCount(ctx, &DB{}, "public", "")
	require.Error(t, err)
}

func TestSQLConsoleACLLimitsRole(t *testing.T) {
	db, err := NewFromConfigWithITestID(t)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}

	ctx := context.Background()
	_, err = AdminQuery(ctx, db, `SELECT 1`)
	require.NoError(t, err)

	_, err = AdminQuery(ctx, db, `SELECT address FROM eth_keys`)
	require.Error(t, err)

	_, err = AdminQuery(ctx, db, `INSERT INTO harmony_task_history (task_id, name, posted, result) VALUES (0, 'x', NOW(), true)`)
	require.Error(t, err)
}
