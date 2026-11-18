package webrpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func insertRequestDeletionDataSet(t *testing.T, ctx context.Context, db *harmonydb.DB, id int64) {
	t.Helper()
	service := fmt.Sprintf("request-deletion-%d", id)
	_, err := db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte(service), service)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES ($1, $2, $3, 100, 10, FALSE)
	`, id, fmt.Sprintf("0x%064x", id), service)
	require.NoError(t, err)
}

func TestIntegration_PDPDataSetRequestDeletion(t *testing.T) {
	const (
		freshDataSet           = int64(920_000_000_001)
		unrequestedRowDataSet  = int64(920_000_000_002)
		clientRequestedDataSet = int64(920_000_000_003)
		unknownDataSet         = int64(920_000_000_004)
	)

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	rpc := &WebRPC{Deps: &deps.Deps{DB: db}}

	for _, id := range []int64{freshDataSet, unrequestedRowDataSet, clientRequestedDataSet} {
		insertRequestDeletionDataSet(t, ctx, db, id)
	}
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (id, client_requested_termination, termination_extra_data)
		VALUES ($1, FALSE, NULL), ($2, TRUE, '\x01')
	`, unrequestedRowDataSet, clientRequestedDataSet)
	require.NoError(t, err)

	require.NoError(t, rpc.PDPDataSetRequestDeletion(ctx, freshDataSet))
	require.NoError(t, rpc.PDPDataSetRequestDeletion(ctx, unrequestedRowDataSet))
	require.ErrorContains(t, rpc.PDPDataSetRequestDeletion(ctx, freshDataSet), "already requested")
	require.ErrorContains(t, rpc.PDPDataSetRequestDeletion(ctx, clientRequestedDataSet), "already requested")
	require.ErrorContains(t, rpc.PDPDataSetRequestDeletion(ctx, unknownDataSet), "not found")

	var rows []struct {
		ID       int64 `db:"id"`
		Client   bool  `db:"client_requested_termination"`
		Operator bool  `db:"operator_requested_deletion"`
	}
	require.NoError(t, db.Select(ctx, &rows, `
		SELECT id, client_requested_termination, operator_requested_deletion
		FROM pdp_delete_data_set
		ORDER BY id
	`))
	type requester struct{ client, operator bool }
	got := make(map[int64]requester, len(rows))
	for _, row := range rows {
		got[row.ID] = requester{client: row.Client, operator: row.Operator}
	}
	require.Equal(t, map[int64]requester{
		freshDataSet:           {operator: true},
		unrequestedRowDataSet:  {operator: true},
		clientRequestedDataSet: {client: true},
	}, got)
}
