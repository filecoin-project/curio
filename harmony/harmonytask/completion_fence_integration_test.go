//go:build integration

package harmonytask

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type fenceTask struct {
	stubAcceptTask
	entered chan struct{}
	finish  chan bool
}

func (s *fenceTask) Do(context.Context, TaskID, func() bool) (bool, error) {
	close(s.entered)
	if <-s.finish {
		return true, nil
	}
	return false, errors.New("test failure")
}

func fenceHandler(db *harmonydb.DB, owner int) (*taskTypeHandler, *fenceTask, chan schedulerEvent, *atomic.Int32) {
	body := &fenceTask{entered: make(chan struct{}), finish: make(chan bool, 1)}
	ch := make(chan schedulerEvent, 16)
	e := &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), db: db, ownerID: owner,
		reg: &resources.Reg{Resources: resources.Resources{Cpu: 2, Ram: 1 << 30}}},
		schedulerChannel: ch, completionCallbacks: make(map[string][]TaskCompleteFunc)}
	h := &taskTypeHandler{TaskInterface: body, TaskEngine: e,
		TaskTypeDetails: TaskTypeDetails{Name: "Fence", Max: taskhelp.Max(1), MaxFailures: 4},
		accept:          acceptcache.New(time.Hour), running: runregistry.New(), storageFailures: make(map[TaskID]time.Time)}
	e.handlers = []*taskTypeHandler{h}
	callbacks := new(atomic.Int32)
	e.OnTaskComplete("Fence", func(context.Context, TaskID, bool) { callbacks.Add(1) })
	return h, body, ch, callbacks
}

func TestCompletionFence(t *testing.T) {
	port := os.Getenv("CURIO_TASK_TEST_PORT")
	if port == "" {
		t.Skip("set CURIO_TASK_TEST_PORT for a disposable loopback PostgreSQL instance")
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
	for _, tc := range []struct {
		name                                                                      string
		stale, sameOwner, missing, success, preempt, historyFailure, finalFailure bool
	}{
		{name: "stale success", stale: true, success: true},
		{name: "stale failure", stale: true},
		{name: "stale final failure", stale: true, finalFailure: true},
		{name: "same owner reacquisition", stale: true, sameOwner: true, success: true},
		{name: "missing task", missing: true, success: true},
		{name: "normal completion", success: true},
		{name: "normal retry"},
		{name: "final failure", finalFailure: true},
		{name: "preemption preserves retry clock", preempt: true},
		{name: "history failure rollback", success: true, historyFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(ctx, `DELETE FROM harmony_task`)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `DELETE FROM harmony_task_history`)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `DELETE FROM harmony_machines`)
			require.NoError(t, err)
			var owner int
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ('test',2,1024,0) RETURNING id`).Scan(&owner))
			var id TaskID
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_task(name,added_by,posted_time,update_time,retries) VALUES ('Fence',1,CURRENT_TIMESTAMP - INTERVAL '2 hours',CURRENT_TIMESTAMP - INTERVAL '1 hour',2) RETURNING id`).Scan(&id))
			var updated time.Time
			require.NoError(t, db.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&updated))
			h, body, events, callbacks := fenceHandler(db, owner)
			if tc.finalFailure {
				h.MaxFailures = 3
			}
			h.accept.Add([]int64{int64(id)})
			require.True(t, h.considerWork(workSourcePoller, []task{{ID: id}}, eventEmitter{schedulerChannel: events}))
			waitAcceptanceTestValue(t, body.entered)
			var current *fenceTask
			var currentEvents chan schedulerEvent
			var currentCallbacks *atomic.Int32
			if tc.stale {
				if tc.sameOwner {
					_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=NULL WHERE id=$1`, id)
				} else {
					_, err = db.Exec(ctx, `UPDATE harmony_machines SET last_contact=CURRENT_TIMESTAMP - INTERVAL '1 day' WHERE id=$1`, owner)
					require.NoError(t, err)
					require.Equal(t, 1, resources.CleanupMachines(ctx, db))
					err = db.QueryRow(ctx, `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ('new',2,1024,0) RETURNING id`).Scan(&owner)
				}
				require.NoError(t, err)
				var next *taskTypeHandler
				next, current, currentEvents, currentCallbacks = fenceHandler(db, owner)
				next.accept.Add([]int64{int64(id)})
				require.True(t, next.considerWork(workSourcePoller, []task{{ID: id}}, eventEmitter{schedulerChannel: currentEvents}))
				waitAcceptanceTestValue(t, current.entered)
				t.Cleanup(func() {
					select {
					case current.finish <- true:
					default:
					}
				})
			}
			if tc.missing {
				_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, id)
				require.NoError(t, err)
			}
			if tc.preempt {
				handle, ok := h.running.Get(int64(id))
				require.True(t, ok)
				handle.Preempt()
			}
			if tc.historyFailure {
				_, err = db.Exec(ctx, `CREATE SEQUENCE history_failure_seen`)
				require.NoError(t, err)
				_, err = db.Exec(ctx, `CREATE FUNCTION reject_history() RETURNS trigger AS $$ BEGIN PERFORM nextval('history_failure_seen'); RAISE EXCEPTION 'history failure'; END; $$ LANGUAGE plpgsql`)
				require.NoError(t, err)
				_, err = db.Exec(ctx, `CREATE TRIGGER reject_history BEFORE INSERT ON harmony_task_history FOR EACH ROW EXECUTE FUNCTION reject_history()`)
				require.NoError(t, err)
			}
			body.finish <- tc.success
			if tc.historyFailure {
				require.Eventually(t, func() bool {
					var seen bool
					_ = db.QueryRow(ctx, `SELECT is_called FROM history_failure_seen`).Scan(&seen)
					return seen
				}, 5*time.Second, 10*time.Millisecond)
				var stillOwner int
				require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id=$1`, id).Scan(&stillOwner))
				require.Equal(t, owner, stillOwner)
				_, err = db.Exec(ctx, `DROP TRIGGER reject_history ON harmony_task_history`)
				require.NoError(t, err)
			}
			waitFenceCompleted(t, events)
			var histories int
			require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=$1`, id).Scan(&histories))
			if tc.stale || tc.missing {
				require.Zero(t, histories)
				require.Zero(t, callbacks.Load())
				if tc.stale {
					var gotOwner, retries int
					var gotUpdated time.Time
					require.NoError(t, db.QueryRow(ctx, `SELECT owner_id,retries,update_time FROM harmony_task WHERE id=$1`, id).Scan(&gotOwner, &retries, &gotUpdated))
					require.Equal(t, owner, gotOwner)
					require.Equal(t, 2, retries)
					require.True(t, updated.Equal(gotUpdated))
					current.finish <- true
					waitFenceCompleted(t, currentEvents)
					require.Equal(t, int32(1), currentCallbacks.Load())
				}
			} else {
				require.Equal(t, 1, histories)
				if tc.success {
					require.Equal(t, int32(1), callbacks.Load())
				}
				if !tc.success && !tc.finalFailure {
					var retry task
					retry.ID = id
					require.NoError(t, db.QueryRow(ctx, `SELECT update_time, posted_time, retries FROM harmony_task WHERE id=$1`, id).Scan(&retry.UpdateTime, &retry.PostedTime, &retry.Retries))
					event := waitAcceptanceTestValue(t, events)
					require.Equal(t, schedulerSourceAdded, event.Source)
					require.True(t, retry.UpdateTime.Equal(event.UpdateTime))
					require.Equal(t, retry.Retries, event.Retries)
					local, remote := pipePair()
					peers := peerregistry.New()
					defer peers.Add(1, "peer", local, []string{"Fence"})()
					p := &peering{peers: peers, h: &TaskEngine{schedulerChannel: events}}
					p.TellNewTask("Fence", id, event.Retries, event.PostedTime, event.UpdateTime)
					wire := waitAcceptanceTestValue(t, remote.recvCh)
					require.NoError(t, p.handlePeerMessage("peer", 1, wire))
					require.True(t, taskFromSchedulerEvent(<-events).UpdateTime.Equal(retry.UpdateTime))
				}
				if tc.finalFailure || tc.success {
					var count int
					require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task WHERE id=$1`, id).Scan(&count))
					require.Zero(t, count)
				}
				if tc.preempt {
					var got time.Time
					require.NoError(t, db.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&got))
					require.True(t, updated.Equal(got))
				}
			}
		})
	}
}

func waitFenceCompleted(t *testing.T, events <-chan schedulerEvent) {
	t.Helper()
	for {
		if waitAcceptanceTestValue(t, events).Source == schedulerSourceTaskCompleted {
			return
		}
	}
}

func TestPeerRetryCommittedTime(t *testing.T) {
	updated := time.Now().Add(-time.Hour).UTC()
	wire, err := json.Marshal(map[string]any{"verb": "newTask", "taskID": 42,
		"other": map[string]any{"taskType": "Fence", "retries": 1, "posted": updated, "updateTime": updated}})
	require.NoError(t, err)
	events := make(chan schedulerEvent, 1)
	p := &peering{h: &TaskEngine{schedulerChannel: events}, peers: peerregistry.New()}
	require.NoError(t, p.handlePeerMessage("peer", 1, wire))
	require.True(t, taskFromSchedulerEvent(<-events).UpdateTime.Equal(updated))
}
