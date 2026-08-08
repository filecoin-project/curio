package harmonytask

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

func TestSingletonTaskDisabled(t *testing.T) {
	db := getDB(t)
	ctx := context.Background()

	tk := newTestTask("SingletonDisabledTest", 5)
	tk.iAmBored = harmonytask.SingletonTaskAdder(300*time.Millisecond, tk)
	t.Cleanup(cleanupTasks(tk))

	_, err := db.Exec(ctx, `INSERT INTO harmony_task_singleton_disabled (task_name, reason) VALUES ($1, $2)`,
		tk.name, "test: repair complete")
	require.NoError(t, err)

	makeEngine(t, db, []harmonytask.TaskInterface{tk}, "singdis:1000")

	select {
	case id := <-tk.doneCh:
		t.Fatalf("disabled singleton task should not have been scheduled, got task %d", id)
	case <-time.After(3 * time.Second):
	}

	_, err = db.Exec(ctx, `DELETE FROM harmony_task_singleton_disabled WHERE task_name = $1`, tk.name)
	require.NoError(t, err)

	id := waitForTask(t, tk.doneCh, taskTimeout)
	require.NotZero(t, id)
}
