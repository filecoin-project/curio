//go:build integration

package harmonytask

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type startReservationTask struct {
	stubAcceptTask
	accept, unpaced  bool
	entered          chan struct{}
	finish           chan bool
	beforeStart      chan struct{}
	proceed          chan struct{}
	starts, releases atomic.Int32
}

func (s *startReservationTask) Do(context.Context, TaskID, func() bool) (bool, error) {
	close(s.entered)
	if <-s.finish {
		return true, nil
	}
	return false, errors.New("test failure")
}

func (s *startReservationTask) GetSectorID(*harmonydb.DB, int64) (*abi.SectorID, error) {
	if s.beforeStart != nil {
		close(s.beforeStart)
		<-s.proceed
	}
	return nil, nil
}

func (s *startReservationTask) ReserveTaskStart(TaskID) (func(context.Context) error, func(), bool) {
	if s.unpaced {
		return nil, nil, true
	}
	return func(ctx context.Context) error { s.starts.Add(1); return ctx.Err() }, func() { s.releases.Add(1) }, s.accept
}

type failedStartStorage struct{}

func (failedStartStorage) HasCapacity() bool { return true }
func (failedStartStorage) Claim(int) (func() error, error) {
	return nil, errors.New("storage unavailable")
}

func startReservationHandler(db *harmonydb.DB, owner int) (*taskTypeHandler, *startReservationTask, chan schedulerEvent, *atomic.Int32) {
	body := &startReservationTask{accept: true, entered: make(chan struct{}), finish: make(chan bool, 1)}
	ch := make(chan schedulerEvent, 16)
	e := &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), db: db, ownerID: owner,
		reg: &resources.Reg{Resources: resources.Resources{Cpu: 2, Ram: 1 << 30}}},
		schedulerChannel: ch, completionCallbacks: make(map[string][]TaskCompleteFunc)}
	h := &taskTypeHandler{TaskInterface: body, TaskEngine: e,
		TaskTypeDetails: TaskTypeDetails{Name: "Start", Max: taskhelp.Max(1), MaxFailures: 4},
		accept:          acceptcache.New(time.Hour), running: runregistry.New(), storageFailures: make(map[TaskID]time.Time)}
	e.handlers = []*taskTypeHandler{h}
	callbacks := new(atomic.Int32)
	e.OnTaskComplete("Start", func(context.Context, TaskID, bool) { callbacks.Add(1) })
	return h, body, ch, callbacks
}

func waitStartCompleted(t *testing.T, ch <-chan schedulerEvent) {
	t.Helper()
	for waitAcceptanceTestValue(t, ch).Source != schedulerSourceTaskCompleted {
	}
}

func TestStartReservation(t *testing.T) {
	port := os.Getenv("CURIO_TASK_TEST_PORT")
	if port == "" {
		t.Skip("requires disposable loopback PostgreSQL")
	}
	db, err := harmonydb.NewFromConfigWithITestID(t, func(o *harmonydb.ItestOptions) {
		o.Hosts, o.Port = []string{"127.0.0.1"}, port
		o.Database, o.Username, o.Password = "postgres", "postgres", ""
	})
	require.NoError(t, err)
	ctx := context.Background()
	t.Run("unpaced skips candidate claimed by another worker", func(t *testing.T) {
		var owner, peerOwner int
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ('unpaced',2,1024,0) RETURNING id`).Scan(&owner))
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ('peer',2,1024,0) RETURNING id`).Scan(&peerOwner))
		var a, b TaskID
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_task(name,added_by,posted_time) VALUES ('Start',1,CURRENT_TIMESTAMP) RETURNING id`).Scan(&a))
		require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_task(name,added_by,posted_time) VALUES ('Start',1,CURRENT_TIMESTAMP) RETURNING id`).Scan(&b))
		h, body, events, callbacks := startReservationHandler(db, owner)
		body.unpaced = true
		h.accept.Add([]int64{int64(a), int64(b)})

		// Both candidates passed acceptance, but a peer claims A before our SQL claim.
		peer, peerBody, peerEvents, _ := startReservationHandler(db, peerOwner)
		peer.accept.Add([]int64{int64(a)})
		require.True(t, peer.considerWork(workSourcePoller, []task{{ID: a}}, eventEmitter{schedulerChannel: peerEvents}))
		waitAcceptanceTestValue(t, peerBody.entered)
		defer func() {
			peerBody.finish <- true
			waitStartCompleted(t, peerEvents)
		}()

		require.True(t, h.considerWork(workSourcePoller, []task{{ID: a}, {ID: b}}, eventEmitter{schedulerChannel: events}), "SQL must skip owned A and claim unowned B in the same pass")
		waitAcceptanceTestValue(t, body.entered)
		defer func() {
			body.finish <- true
			waitStartCompleted(t, events)
			require.Equal(t, int32(1), callbacks.Load())
		}()
		var actualOwner int
		require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id=$1`, a).Scan(&actualOwner))
		require.Equal(t, peerOwner, actualOwner)
		require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id=$1`, b).Scan(&actualOwner))
		require.Equal(t, owner, actualOwner)
		running := h.running.Snapshot()
		require.Len(t, running, 1)
		require.Equal(t, int64(b), running[0].ID)
	})
	for _, name := range []string{"denied", "claim lost", "claim error", "storage failure", "start canceled", "Do failure"} {
		t.Run(name, func(t *testing.T) {
			var owner int
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_machines(host_and_port,cpu,ram,gpu) VALUES ($1,2,1024,0) RETURNING id`, name).Scan(&owner))
			var id TaskID
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO harmony_task(name,added_by,posted_time,retries) VALUES ('Start',1,CURRENT_TIMESTAMP,2) RETURNING id`).Scan(&id))
			h, body, events, callbacks := startReservationHandler(db, owner)
			body.accept = name != "denied"
			if name == "start canceled" {
				body.beforeStart, body.proceed = make(chan struct{}), make(chan struct{})
			}
			if name == "claim lost" {
				_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=$1 WHERE id=$2`, owner, id)
				require.NoError(t, err)
			}
			if name == "claim error" {
				var cancel context.CancelFunc
				h.TaskEngine.cfg.ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if name == "storage failure" {
				h.Cost.Storage = failedStartStorage{}
			}
			h.accept.Add([]int64{int64(id)})
			accepted := h.considerWork(workSourcePoller, []task{{ID: id, Retries: 2}}, eventEmitter{schedulerChannel: events})
			if name == "start canceled" || name == "Do failure" {
				require.True(t, accepted)
				if name == "start canceled" {
					waitAcceptanceTestValue(t, body.beforeStart)
					handle, ok := h.running.Get(int64(id))
					require.True(t, ok)
					handle.Preempt()
					close(body.proceed)
				} else {
					waitAcceptanceTestValue(t, body.entered)
					body.finish <- false
				}
				waitStartCompleted(t, events)
				require.Equal(t, int32(1), body.starts.Load())
			} else {
				require.False(t, accepted)
				require.Zero(t, body.starts.Load())
			}
			if name != "Do failure" {
				select {
				case <-body.entered:
					t.Fatal("Do entered before a successful start")
				default:
				}
			}
			if name == "denied" {
				require.Zero(t, body.releases.Load())
			} else {
				require.Eventually(t, func() bool { return body.releases.Load() == 1 }, time.Second, time.Millisecond)
			}
			require.Zero(t, callbacks.Load())
			var retries int
			require.NoError(t, db.QueryRow(ctx, `SELECT retries FROM harmony_task WHERE id=$1`, id).Scan(&retries))
			if name == "Do failure" {
				require.Equal(t, 3, retries)
			} else {
				require.Equal(t, 2, retries)
			}
		})
	}
}
