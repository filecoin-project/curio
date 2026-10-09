//go:build integration && !skiff

package seal

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"
)

func TestPrecommitSQLSectorFailureIsolation(t *testing.T) {
	ctx, conn := newPrecommitSQLFixture(t)
	seedPrecommitSQLRows(t, ctx, conn)
	_, err := conn.Exec(ctx, `UPDATE sectors_sdr_pipeline SET task_id_precommit_msg=77;
		UPDATE sectors_sdr_pipeline SET task_id_precommit_msg=88 WHERE sp_id=1000 AND sector_number=4`)
	require.NoError(t, err)
	before := precommitSQLSnapshot(t, ctx, conn)
	for _, identity := range [][3]int64{{88, 1000, 2}, {77, 2000, 3}, {77, 1000, 1}} {
		tag, err := conn.Exec(ctx, SUBMIT_PRECOMMIT_FAIL_SECTOR_SQL, "past-start-epoch", "test failure",
			identity[0], identity[1], identity[2])
		require.NoError(t, err)
		require.Zero(t, tag.RowsAffected())
	}
	require.Equal(t, before, precommitSQLSnapshot(t, ctx, conn), "wrong identities and previously failed evidence must be unchanged")

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	require.NoError(t, err)
	defer rollbackPrecommitSQL(t, tx)
	tag, err := tx.Exec(ctx, SUBMIT_PRECOMMIT_FAIL_SECTOR_SQL, "past-start-epoch", "test failure",
		int64(77), int64(1000), int64(2))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	tag, err = tx.Exec(ctx, SUBMIT_PRECOMMIT_SET_MESSAGE_CID_SQL, "test-message", int64(77), int64(1000), []int64{2, 3, 4})
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "only valid sector 3 is still assigned to this task")
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, before, precommitSQLSnapshot(t, ctx, conn), "failure and CID statements must roll back together when their transaction aborts")

	tag, err = conn.Exec(ctx, SUBMIT_PRECOMMIT_FAIL_SECTOR_SQL, "past-start-epoch", "test failure",
		int64(77), int64(1000), int64(2))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
	var failed, recordedTime bool
	var task *int64
	var reason, message string
	require.NoError(t, conn.QueryRow(ctx, `SELECT failed,failed_at IS NOT NULL,task_id_precommit_msg,failed_reason,failed_reason_msg
		FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=2`).Scan(&failed, &recordedTime, &task, &reason, &message))
	require.True(t, failed)
	require.True(t, recordedTime)
	require.Nil(t, task)
	require.Equal(t, "past-start-epoch", reason)
	require.Equal(t, "test failure", message)
	var unaffected int
	require.NoError(t, conn.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
		WHERE failed=FALSE AND task_id_precommit_msg=77`).Scan(&unaffected))
	require.Equal(t, 3, unaffected, "other sector/provider identities remain valid and assigned")
	tag, err = conn.Exec(ctx, SUBMIT_PRECOMMIT_FAIL_SECTOR_SQL, "precommit-check", "must not overwrite",
		int64(77), int64(1000), int64(2))
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected())
}
