package pdpv0

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ethchain"
)

const (
	gateClientDataSet      = int64(910_000_000_001)
	gateOperatorDataSet    = int64(910_000_000_002)
	gateUnrequestedDataSet = int64(910_000_000_003)
)

type gateBlockNumberClient struct {
	ethchain.EthClient
	block uint64
}

func (c gateBlockNumberClient) BlockNumber(context.Context) (uint64, error) {
	return c.block, nil
}

func gateAddTaskFunc(t *testing.T, ctx context.Context, db *harmonydb.DB) harmonytask.AddTaskFunc {
	t.Helper()
	return func(extraInfo func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			var taskID int64
			err := tx.QueryRow(`
				INSERT INTO harmony_task (posted_time, added_by, name)
				VALUES (NOW(), 1, 'test')
				RETURNING id
			`).Scan(&taskID)
			if err != nil {
				return false, err
			}
			return extraInfo(harmonytask.TaskID(taskID), tx)
		})
		require.NoError(t, err)
	}
}

func insertGateRows(t *testing.T, ctx context.Context, db *harmonydb.DB, readyForDelete bool) {
	t.Helper()
	var epoch sql.NullInt64
	if readyForDelete {
		epoch = sql.NullInt64{Int64: 100, Valid: true}
	}
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (
			id, client_requested_termination, termination_extra_data, operator_requested_deletion,
			after_terminate_service, service_termination_epoch, deletion_allowed
		)
		VALUES
			($1, TRUE, '\x01', FALSE, $4, $5, $4),
			($2, FALSE, NULL, TRUE, $4, $5, $4),
			($3, FALSE, NULL, FALSE, $4, $5, $4)
	`, gateClientDataSet, gateOperatorDataSet, gateUnrequestedDataSet, readyForDelete, epoch)
	require.NoError(t, err)
}

type gateSchedule struct {
	terminate map[int64]bool
	delete    map[int64]bool
}

func loadGateSchedule(t *testing.T, ctx context.Context, db *harmonydb.DB) gateSchedule {
	t.Helper()
	var rows []struct {
		ID        int64 `db:"id"`
		Terminate bool  `db:"terminate_scheduled"`
		Delete    bool  `db:"delete_scheduled"`
	}
	require.NoError(t, db.Select(ctx, &rows, `
		SELECT id,
		       terminate_service_task_id IS NOT NULL AS terminate_scheduled,
		       delete_data_set_task_id IS NOT NULL AS delete_scheduled
		FROM pdp_delete_data_set
	`))
	out := gateSchedule{terminate: map[int64]bool{}, delete: map[int64]bool{}}
	for _, row := range rows {
		out.terminate[row.ID] = row.Terminate
		out.delete[row.ID] = row.Delete
	}
	return out
}

func TestIntegration_TerminateFWSSSchedule_OnlyRequestedDeletions(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	insertGateRows(t, ctx, db, false)

	task := &TerminateFWSSTask{db: db}
	require.NoError(t, task.schedule(ctx, gateAddTaskFunc(t, ctx, db)))

	require.Equal(t, map[int64]bool{
		gateClientDataSet:      true,
		gateOperatorDataSet:    true,
		gateUnrequestedDataSet: false,
	}, loadGateSchedule(t, ctx, db).terminate)
}

func TestIntegration_DeleteDataSetSchedule_OnlyRequestedDeletions(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	insertGateRows(t, ctx, db, true)

	task := &DeleteDataSetTask{db: db, ethClient: gateBlockNumberClient{block: 200}}
	require.NoError(t, task.schedule(ctx, gateAddTaskFunc(t, ctx, db)))

	require.Equal(t, map[int64]bool{
		gateClientDataSet:      true,
		gateOperatorDataSet:    true,
		gateUnrequestedDataSet: false,
	}, loadGateSchedule(t, ctx, db).delete)
}
