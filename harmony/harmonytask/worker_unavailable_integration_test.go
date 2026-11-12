//go:build integration

package harmonytask

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type unavailableTestTask struct {
	stubAcceptTask
	entered chan struct{}
	finish  chan error
}

func (s *unavailableTestTask) Do(context.Context, TaskID, func() bool) (bool, error) {
	close(s.entered)
	err := <-s.finish
	return err == nil, err
}

func unavailableTestHandler(t *testing.T, db *harmonydb.DB) (*taskTypeHandler, *unavailableTestTask, *atomic.Int32) {
	t.Helper()
	body := &unavailableTestTask{entered: make(chan struct{}), finish: make(chan error, 1)}
	e := newAcceptanceTestEngine(t, &body.stubAcceptTask)
	e.cfg.db = db
	require.NoError(t, db.QueryRow(context.Background(), `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ($1,2,1024,0) RETURNING id`, t.Name()+time.Now().String()).Scan(&e.cfg.ownerID))
	e.completionCallbacks = make(map[string][]TaskCompleteFunc)
	h := e.handlers[0]
	h.TaskInterface = body
	h.Max, h.MaxFailures = taskhelp.Max(1), 3
	h.storageFailures = make(map[TaskID]time.Time)
	callbacks := new(atomic.Int32)
	e.OnTaskComplete(h.Name, func(context.Context, TaskID, bool) { callbacks.Add(1) })
	return h, body, callbacks
}

func TestWorkerUnavailableCompletion(t *testing.T) {
	port := os.Getenv("CURIO_TASK_TEST_PORT")
	if port == "" {
		t.Skip("requires disposable loopback PostgreSQL")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	require.NotZero(t, n)
	db, err := harmonydb.NewFromConfigWithITestID(t, func(o *harmonydb.ItestOptions) {
		o.Hosts, o.Port = []string{"127.0.0.1"}, port
		o.Database, o.Username, o.Password = "postgres", "postgres", ""
	})
	require.NoError(t, err)
	ctx := context.Background()
	unavailable := &taskhelp.WorkerUnavailable{Cause: errors.New("No CUDA devices available")}
	for _, tc := range []struct {
		name                                   string
		err                                    error
		retries                                int
		preempt, historyFailure, commitFailure bool
	}{
		{name: "unavailable at failure limit", err: unavailable, retries: 2},
		{name: "history rollback", err: unavailable, retries: 2, historyFailure: true},
		{name: "commit rollback", err: unavailable, retries: 2, commitFailure: true},
		{name: "ordinary retry", err: errors.New("proof failure"), retries: 1},
		{name: "ordinary final failure", err: errors.New("proof failure"), retries: 2},
		{name: "preemption", err: context.Canceled, retries: 2, preempt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, body, callbacks := unavailableTestHandler(t, db)
			events := h.TaskEngine.schedulerChannel
			var id TaskID
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_task(name,added_by,posted_time,update_time,retries) VALUES ($1,$2,now()-interval '2 hours',now()-interval '1 hour',$3) RETURNING id`, h.Name, h.TaskEngine.cfg.ownerID, tc.retries).Scan(&id))
			var updated time.Time
			require.NoError(t, db.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&updated))
			h.accept.Add([]int64{int64(id)})
			// Deliberately stale discovery metadata: the DB's retry budget is authoritative.
			discoveredRetries := tc.retries
			if tc.err == unavailable {
				discoveredRetries = 0
			}
			require.True(t, h.considerWork(workSourcePoller, []task{{ID: id, Retries: discoveredRetries, UpdateTime: updated}}, eventEmitter{schedulerChannel: events}))
			waitAcceptanceTestValue(t, body.entered)
			require.Equal(t, schedulerSourceTaskStarted, waitAcceptanceTestValue(t, events).Source)
			if tc.preempt {
				handle, ok := h.running.Get(int64(id))
				require.True(t, ok)
				handle.Preempt()
			}
			if tc.historyFailure || tc.commitFailure {
				_, err := db.Exec(ctx, `CREATE SEQUENCE unavailable_attempts; CREATE FUNCTION unavailable_history_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM nextval('unavailable_attempts'); RAISE EXCEPTION 'injected completion failure'; END $$`)
				require.NoError(t, err)
				if tc.commitFailure {
					_, err = db.Exec(ctx, `CREATE CONSTRAINT TRIGGER unavailable_history_failure AFTER INSERT ON harmony_task_history DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION unavailable_history_failure()`)
				} else {
					_, err = db.Exec(ctx, `CREATE TRIGGER unavailable_history_failure BEFORE INSERT ON harmony_task_history FOR EACH ROW EXECUTE FUNCTION unavailable_history_failure()`)
				}
				require.NoError(t, err)
			}
			body.finish <- tc.err
			if tc.historyFailure || tc.commitFailure {
				require.Eventually(t, func() bool {
					var attempts int
					return db.QueryRow(ctx, `SELECT last_value FROM unavailable_attempts`).Scan(&attempts) == nil && attempts >= 2
				}, 5*time.Second, 10*time.Millisecond)
				require.Empty(t, events, "completion/re-notification must wait for commit")
				var owner, retries, histories int
				var current time.Time
				require.NoError(t, db.QueryRow(ctx, `SELECT owner_id,retries,update_time FROM harmony_task WHERE id=$1`, id).Scan(&owner, &retries, &current))
				require.Equal(t, h.TaskEngine.cfg.ownerID, owner)
				require.Equal(t, tc.retries, retries)
				require.True(t, updated.Equal(current))
				require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=$1`, id).Scan(&histories))
				require.Zero(t, histories)
				_, err := db.Exec(ctx, `DROP TRIGGER unavailable_history_failure ON harmony_task_history; DROP FUNCTION unavailable_history_failure(); DROP SEQUENCE unavailable_attempts`)
				require.NoError(t, err)
			}
			require.Equal(t, schedulerSourceTaskCompleted, waitAcceptanceTestValue(t, events).Source)
			require.Zero(t, callbacks.Load())
			var histories int
			require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=$1 AND NOT result`, id).Scan(&histories))
			require.Equal(t, 1, histories)
			if tc.err != unavailable && !tc.preempt && tc.retries == 2 {
				var rows int
				require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task WHERE id=$1`, id).Scan(&rows))
				require.Zero(t, rows)
				require.Empty(t, events)
				return
			}
			var owner *int
			var retries int
			var current time.Time
			require.NoError(t, db.QueryRow(ctx, `SELECT owner_id,retries,update_time FROM harmony_task WHERE id=$1`, id).Scan(&owner, &retries, &current))
			require.Nil(t, owner)
			expected := tc.retries
			if tc.err != unavailable && !tc.preempt {
				expected++
			}
			require.Equal(t, expected, retries)
			if tc.err == unavailable {
				require.True(t, updated.Equal(current))
			} else {
				require.True(t, current.After(updated))
			}
			notice := waitAcceptanceTestValue(t, events)
			require.Equal(t, schedulerSourceAdded, notice.Source)
			require.Equal(t, id, notice.TaskID)
			require.Equal(t, expected, notice.Retries)
			if tc.err == unavailable {
				require.True(t, updated.Equal(notice.UpdateTime))
				healthy, healthyBody, _ := unavailableTestHandler(t, db)
				healthy.accept.Add([]int64{int64(id)})
				require.True(t, healthy.considerWork(workSourcePoller, []task{taskFromSchedulerEvent(notice)}, eventEmitter{schedulerChannel: healthy.TaskEngine.schedulerChannel}))
				waitAcceptanceTestValue(t, healthyBody.entered)
				healthyBody.finish <- nil
				require.Equal(t, schedulerSourceTaskStarted, waitAcceptanceTestValue(t, healthy.TaskEngine.schedulerChannel).Source)
				require.True(t, waitAcceptanceTestValue(t, healthy.TaskEngine.schedulerChannel).Success, "healthy worker did not finish")
			}
		})
	}
}
