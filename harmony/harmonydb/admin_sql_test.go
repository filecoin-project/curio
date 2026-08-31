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
