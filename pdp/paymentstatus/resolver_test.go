package paymentstatus

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func TestIntegration_LoadOverlayBundles_PipelineMembership(t *testing.T) {
	const (
		noRow                   = int64(930_000_000_001)
		unrequestedBeforeDelete = int64(930_000_000_002)
		operatorRequested       = int64(930_000_000_003)
		clientRequested         = int64(930_000_000_004)
		unrequestedDeleteSent   = int64(930_000_000_005)
		unrequestedDeleteQueued = int64(930_000_000_006)
		requestedTerminated     = int64(930_000_000_007)
	)
	ids := []int64{
		noRow, unrequestedBeforeDelete, operatorRequested, clientRequested,
		unrequestedDeleteSent, unrequestedDeleteQueued, requestedTerminated,
	}

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	for _, id := range ids {
		service := fmt.Sprintf("overlay-%d", id)
		_, err := db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte(service), service)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `
			INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
			VALUES ($1, $2, $3, 100, 10, FALSE)
		`, id, fmt.Sprintf("0x%064x", id), service)
		require.NoError(t, err)
	}

	var deleteTaskID int64
	err = db.QueryRow(ctx, `
		INSERT INTO harmony_task (posted_time, added_by, name)
		VALUES (NOW(), 1, 'test')
		RETURNING id
	`).Scan(&deleteTaskID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (
			id, client_requested_termination, termination_extra_data, operator_requested_deletion,
			after_delete_data_set, delete_data_set_task_id, terminated
		)
		VALUES
			($1, FALSE, NULL, FALSE, FALSE, NULL, FALSE),
			($2, FALSE, NULL, TRUE, FALSE, NULL, FALSE),
			($3, TRUE, '\x01', FALSE, FALSE, NULL, FALSE),
			($4, FALSE, NULL, FALSE, TRUE, NULL, FALSE),
			($5, FALSE, NULL, FALSE, FALSE, $7, FALSE),
			($6, FALSE, NULL, TRUE, FALSE, NULL, TRUE)
	`, unrequestedBeforeDelete, operatorRequested, clientRequested,
		unrequestedDeleteSent, unrequestedDeleteQueued, requestedTerminated, deleteTaskID)
	require.NoError(t, err)

	bundles, err := loadOverlayBundles(ctx, db, ids)
	require.NoError(t, err)

	inPipeline := make(map[int64]bool, len(bundles))
	for id, bundle := range bundles {
		inPipeline[id] = bundle.delete.InPipeline
	}
	require.Equal(t, map[int64]bool{
		noRow:                   false,
		unrequestedBeforeDelete: false,
		operatorRequested:       true,
		clientRequested:         true,
		unrequestedDeleteSent:   true,
		unrequestedDeleteQueued: true,
		requestedTerminated:     false,
	}, inPipeline)

	require.True(t, bundles[operatorRequested].delete.OperatorRequested)
	require.False(t, bundles[operatorRequested].delete.ClientRequested)
	require.True(t, bundles[clientRequested].delete.ClientRequested)
	require.False(t, bundles[clientRequested].delete.OperatorRequested)
}
