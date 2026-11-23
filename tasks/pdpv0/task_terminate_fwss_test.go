package pdpv0

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

const terminateTestHead = uint64(7000)

// terminateChainClient adds a chain head to the contract call answers.
type terminateChainClient struct {
	*chainSyncViewClient
}

func (terminateChainClient) BlockNumber(context.Context) (uint64, error) {
	return terminateTestHead, nil
}

type terminateTestRow struct {
	AfterTerminate   bool           `db:"after_terminate_service"`
	TerminationEpoch sql.NullInt64  `db:"service_termination_epoch"`
	DeletionAllowed  bool           `db:"deletion_allowed"`
	TerminateTxHash  sql.NullString `db:"terminate_tx_hash"`
	AfterDelete      bool           `db:"after_delete_data_set"`
	DeleteTxHash     sql.NullString `db:"delete_tx_hash"`
}

func loadTerminateTestRow(t *testing.T, ctx context.Context, db *harmonydb.DB, dataSetID int64) terminateTestRow {
	t.Helper()
	var rows []terminateTestRow
	require.NoError(t, db.Select(ctx, &rows, `
		SELECT after_terminate_service, service_termination_epoch, deletion_allowed,
		       terminate_tx_hash, after_delete_data_set, delete_tx_hash
		FROM pdp_delete_data_set
		WHERE id = $1
	`, dataSetID))
	require.Len(t, rows, 1)
	return rows[0]
}

type terminateTestTask struct {
	ID int64 `db:"task_id"`
}

// runTerminateTestTasks runs do once for each of the two scheduled tasks.
func runTerminateTestTasks(t *testing.T, ctx context.Context, tasks []terminateTestTask, do func(context.Context, harmonytask.TaskID, func() bool) (bool, error)) {
	t.Helper()
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		done, err := do(ctx, harmonytask.TaskID(task.ID), func() bool { return true })
		require.NoError(t, err)
		require.True(t, done)
	}
}

func TestIntegration_TerminateFWSS_DataSetGoneOnChain(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	removed := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	inCleanup := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (id, operator_requested_deletion)
		VALUES ($1, TRUE), ($2, TRUE)
	`, removed, inCleanup)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO eth_keys (address, private_key, role)
		VALUES ('0x00000000000000000000000000000000000c5a13', '\x01', 'pdp')
	`)
	require.NoError(t, err)

	// FWSS has no record of either data set. PDPVerifier has dropped the
	// first and still holds pieces of the second to clean up.
	view := newChainSyncViewClient(t)
	view.setDataSet(removed, 0, 0)
	view.setDataSet(inCleanup, 0, 0)
	view.setRemoved(removed)
	view.setCleanupMode(inCleanup)
	client := terminateChainClient{view}
	addTask := gateAddTaskFunc(t, ctx, db)

	// No sender is set: sending terminateService would panic.
	terminate := &TerminateFWSSTask{db: db, ethClient: client}
	require.NoError(t, terminate.schedule(ctx, addTask))
	var terminateTasks []terminateTestTask
	require.NoError(t, db.Select(ctx, &terminateTasks, `
		SELECT terminate_service_task_id AS task_id
		FROM pdp_delete_data_set
		WHERE terminate_service_task_id IS NOT NULL
	`))
	runTerminateTestTasks(t, ctx, terminateTasks, terminate.Do)

	for _, dataSetID := range []int64{removed, inCleanup} {
		row := loadTerminateTestRow(t, ctx, db, dataSetID)
		require.True(t, row.AfterTerminate)
		require.True(t, row.DeletionAllowed)
		require.Equal(t, sql.NullInt64{Int64: int64(terminateTestHead), Valid: true}, row.TerminationEpoch)
		require.False(t, row.TerminateTxHash.Valid)
	}

	// The deleteDataSet stage takes both from there without a transaction.
	deleteTask := &DeleteDataSetTask{db: db, ethClient: client}
	require.NoError(t, deleteTask.schedule(ctx, addTask))
	var deleteTasks []terminateTestTask
	require.NoError(t, db.Select(ctx, &deleteTasks, `
		SELECT delete_data_set_task_id AS task_id
		FROM pdp_delete_data_set
		WHERE delete_data_set_task_id IS NOT NULL
	`))
	runTerminateTestTasks(t, ctx, deleteTasks, deleteTask.Do)

	require.Equal(t, map[int64]bool{inCleanup: true}, loadKeptDataSets(t, ctx, db))
	require.False(t, loadTerminateTestRow(t, ctx, db, inCleanup).DeleteTxHash.Valid)
}
