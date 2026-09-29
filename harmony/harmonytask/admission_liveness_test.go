package harmonytask

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type admissionPhaseStore struct {
	taskAttemptStore
	prepareFn func(context.Context, TaskID, string) error
	releaseFn func(context.Context, TaskID) error
}

func (s admissionPhaseStore) prepare(ctx context.Context, id TaskID, token string) error {
	if s.prepareFn != nil {
		return s.prepareFn(ctx, id, token)
	}
	return s.taskAttemptStore.prepare(ctx, id, token)
}
func (s admissionPhaseStore) releaseUnstarted(ctx context.Context, id TaskID) error {
	if s.releaseFn != nil {
		return s.releaseFn(ctx, id)
	}
	return s.taskAttemptStore.releaseUnstarted(ctx, id)
}

type admissionDoTask struct {
	stubAcceptTask
	entered      chan TaskID
	finish       <-chan struct{}
	ignoreCancel bool
}

func (s *admissionDoTask) Do(ctx context.Context, id TaskID, _ func() bool) (bool, error) {
	s.entered <- id
	if s.ignoreCancel {
		<-s.finish
		return true, nil
	}
	select {
	case <-s.finish:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func receiveAdmission[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("admission progress barrier timed out")
	}
	var zero T
	return zero
}

func newAdmissionEngine(t *testing.T, cpu int) (*TaskEngine, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e := &TaskEngine{cfg: taskEngineConfig{ctx: ctx, grace: cancel, ownerID: 7, reg: &resources.Reg{Resources: resources.Resources{Cpu: cpu, Ram: 1 << 30}}},
		taskMap: map[string]*taskTypeHandler{}, schedulerChannel: make(chan schedulerEvent, 128), admissionWake: make(chan struct{}, 1), recovery: map[string][]task{}, peering: &peering{peers: peerregistry.New()}}
	return e, cancel
}

func addAdmissionHandler(e *TaskEngine, name string, limit taskhelp.Limiter, store taskAttemptStore, finish <-chan struct{}) (*taskTypeHandler, *admissionDoTask) {
	impl := &admissionDoTask{entered: make(chan TaskID, 256), finish: finish}
	h := &taskTypeHandler{TaskInterface: impl, TaskEngine: e, TaskTypeDetails: TaskTypeDetails{Name: name, Max: limit.Instance(), MaxFailures: 1, Cost: resources.Resources{Cpu: 1}},
		storageFailures: map[TaskID]time.Time{}, running: runregistry.New(), accept: acceptcache.New(time.Hour), completionRecorder: func(TaskID, bool, error) {}}
	h.admissionFactory = func(string, []task) (func([]TaskID, int) ([]TaskID, error), taskAttemptStore) {
		return func(ids []TaskID, limit int) ([]TaskID, error) { return ids[:min(len(ids), limit)], nil }, store
	}
	e.handlers = append(e.handlers, h)
	e.taskMap[name] = h
	return h, impl
}

func TestAdmissionCancellationLateSuccessAndShutdown(t *testing.T) {
	for _, mode := range []string{"preempt", "cordon", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			e, cancel := newAdmissionEngine(t, 2)
			defer cancel()
			blocked, release := make(chan struct{}), make(chan struct{})
			base := newMemoryAttemptStore()
			store := admissionPhaseStore{taskAttemptStore: base, prepareFn: func(_ context.Context, id TaskID, token string) error {
				close(blocked)
				<-release
				// The driver committed but ignored cancellation/delivered late success.
				return base.prepare(context.Background(), id, token)
			}}
			finish := make(chan struct{})
			defer close(finish)
			h, impl := addAdmissionHandler(e, "Late", taskhelp.Max(1), store, finish)
			h.TimeSensitive = true
			h.Uninterruptible = true
			require.True(t, h.considerWork(workSourcePoller, []task{{ID: 1}}, eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}))
			a := h.admissions[1]
			receiveAdmission(t, blocked)
			require.Equal(t, 1, h.Max.Active())
			require.Equal(t, 1, e.ResourcesAvailable().Cpu)
			switch mode {
			case "preempt":
				plan := e.computePreemptionPlan(resources.Resources{Cpu: 2})
				require.NotNil(t, plan)
				e.executePreemption(plan)
			case "cordon":
				e.atomics.yieldBackground.Store(true)
				e.cancelPendingAdmissions()
			case "shutdown":
				e.atomics.draining.Store(true)
				cancel()
				e.cancelPendingAdmissions()
			}
			receiveAdmission(t, a.localDone)
			require.Zero(t, h.Max.Active())
			require.Equal(t, 2, e.ResourcesAvailable().Cpu)
			close(release)
			receiveAdmission(t, a.workerDone)
			h.drainAdmissions()
			require.Empty(t, impl.entered)
			require.Empty(t, base.starts)
			require.Equal(t, []TaskID{1}, base.released)
			require.Empty(t, h.running.Snapshot())
			// Cleanup/result completion did not need a live scheduler or spare
			// event-channel capacity (this fixture never consumes that channel).
		})
	}
}

// One real deadline expiration, not a shortened production timeout. The other
// delay tests use barriers so they do not each wait five seconds.
func TestAdmissionPreparationDeadlineReturnsCapacity(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 1)
	defer cancel()
	base := newMemoryAttemptStore()
	observed := make(chan error, 1)
	store := admissionPhaseStore{taskAttemptStore: base, prepareFn: func(ctx context.Context, _ TaskID, _ string) error {
		<-ctx.Done()
		observed <- ctx.Err()
		return ctx.Err()
	}}
	finish := make(chan struct{})
	defer close(finish)
	h, impl := addAdmissionHandler(e, "Deadline", taskhelp.Max(1), store, finish)
	require.True(t, h.considerWork(workSourcePoller, []task{{ID: 1}}, eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}))
	a := h.admissions[1]
	select {
	case err := <-observed:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(7 * time.Second):
		a.handle.CancelPending()
		t.Fatal("production preparation deadline did not expire")
	}
	receiveAdmission(t, a.workerDone)
	h.drainAdmissions()
	require.Zero(t, h.Max.Active())
	require.Empty(t, h.running.Snapshot())
	require.Empty(t, impl.entered)
	require.Equal(t, []TaskID{1}, base.released)
}

func TestAdmissionSharedPendingMaxAndRefill(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 4)
	defer cancel()
	blocked, release := make(chan struct{}, 2), make(chan struct{})
	base := newMemoryAttemptStore()
	store := admissionPhaseStore{taskAttemptStore: base, prepareFn: func(ctx context.Context, id TaskID, token string) error {
		blocked <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return base.prepare(ctx, id, token)
	}}
	finish := make(chan struct{})
	defer close(finish)
	shared := taskhelp.Max(2)
	a, _ := addAdmissionHandler(e, "SDR", shared, store, finish)
	b, fast := addAdmissionHandler(e, "SDRKeyRegen", shared, newMemoryAttemptStore(), finish)
	ee := eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}
	require.True(t, a.considerWork(workSourcePoller, []task{{ID: 1}, {ID: 2}, {ID: 3}}, ee))
	receiveAdmission(t, blocked)
	receiveAdmission(t, blocked)
	require.Equal(t, 2, shared.Active())
	require.Equal(t, 2, e.ResourcesAvailable().Cpu)
	require.False(t, b.considerWork(workSourcePoller, []task{{ID: 4}}, ee))
	first := a.admissions[1]
	first.handle.CancelPending()
	receiveAdmission(t, first.workerDone)
	a.drainAdmissions()
	require.Equal(t, 1, shared.Active())
	require.True(t, b.considerWork(workSourcePoller, []task{{ID: 4}}, ee))
	settleAdmissions(t, b)
	require.Equal(t, TaskID(4), receiveAdmission(t, fast.entered))
	require.Equal(t, 2, shared.Active())
	second := a.admissions[2]
	second.handle.CancelPending()
	receiveAdmission(t, second.workerDone)
	close(release)
	cancel()
	e.cancelPendingAdmissions()
}

func TestAdmissionFailureQuarantineIsBoundedAndDoesNotBlockOtherType(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 101)
	defer cancel()
	base := newMemoryAttemptStore()
	store := admissionPhaseStore{taskAttemptStore: base,
		prepareFn: func(context.Context, TaskID, string) error { panic("prepare panic") },
		releaseFn: func(context.Context, TaskID) error { return errors.New("unknown cleanup outcome") }}
	finish := make(chan struct{})
	defer close(finish)
	h, _ := addAdmissionHandler(e, "Failing", taskhelp.Max(0), store, finish)
	tasks := make([]task, 101)
	for i := range tasks {
		tasks[i].ID = TaskID(i + 1)
	}
	ee := eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}
	require.True(t, h.considerWork(workSourcePoller, tasks, ee))
	settleAdmissions(t, h)
	require.Len(t, h.admissions, maxPendingAdmissions)
	require.Zero(t, h.Max.Active())
	require.False(t, h.considerWork(workSourcePoller, tasks, ee))
	b, fast := addAdmissionHandler(e, "Other", taskhelp.Max(1), newMemoryAttemptStore(), finish)
	require.True(t, b.considerWork(workSourcePoller, []task{{ID: 200}}, ee))
	settleAdmissions(t, b)
	require.Equal(t, TaskID(200), receiveAdmission(t, fast.entered))
}

func TestAdmissionBatchPartialFailureAndDuplicateDiscovery(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 4)
	defer cancel()
	base := newMemoryAttemptStore()
	store := admissionPhaseStore{taskAttemptStore: base, prepareFn: func(ctx context.Context, id TaskID, token string) error {
		if id == 1 {
			return errors.New("prepare error")
		}
		if id == 2 {
			panic("prepare panic")
		}
		return base.prepare(ctx, id, token)
	}}
	finish := make(chan struct{})
	defer close(finish)
	h, impl := addAdmissionHandler(e, "Partial", taskhelp.Max(4), store, finish)
	h.Cost.Storage = &admissionFixtureStorage{claim: func(id int) (func() error, error) {
		if id == 3 {
			return nil, errors.New("storage error")
		}
		return func() error { return nil }, nil
	}}
	tasks := []task{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	ee := eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}
	require.True(t, h.considerWork(workSourcePoller, tasks, ee))
	// Recovery/cache/peer rediscovery cannot install another local admission.
	for _, source := range []string{workSourcePoller, workSourceRecover, workSourcePreempt} {
		require.True(t, h.considerWork(source, tasks, ee))
	}
	settleAdmissions(t, h)
	require.Equal(t, TaskID(4), receiveAdmission(t, impl.entered))
	require.Empty(t, impl.entered)
	require.Equal(t, 1, h.Max.Active())
	require.Len(t, h.running.Snapshot(), 1)
	require.ElementsMatch(t, []TaskID{1, 2, 3}, base.released)
}

func TestAdmissionEnteredPreemptionKeepsResourcesUntilExit(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 2)
	defer cancel()
	finish := make(chan struct{})
	h, impl := addAdmissionHandler(e, "Entered", taskhelp.Max(1), newMemoryAttemptStore(), finish)
	impl.ignoreCancel = true
	require.True(t, h.considerWork(workSourcePoller, []task{{ID: 1}}, eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}))
	a := h.admissions[1]
	settleAdmissions(t, h)
	receiveAdmission(t, impl.entered)
	a.handle.Preempt()
	require.Equal(t, 1, h.Max.Active(), "preemption cannot refund a still-running body")
	e.cancelPendingAdmissions()
	require.Equal(t, 1, h.Max.Active())
	close(finish)
	require.True(t, a.handle.WaitDone(time.Now().Add(time.Second)))
	require.Zero(t, h.Max.Active())
}

func TestAdmissionStartupRecoveryUsesSchedulerAndNewEntry(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 2)
	finish := make(chan struct{})
	h, impl := addAdmissionHandler(e, "Recovered", taskhelp.Max(1), newMemoryAttemptStore(), finish)
	var recoveryClaims atomic.Int32
	factory := h.admissionFactory
	h.admissionFactory = func(source string, tasks []task) (func([]TaskID, int) ([]TaskID, error), taskAttemptStore) {
		claim, store := factory(source, tasks)
		return func(ids []TaskID, limit int) ([]TaskID, error) {
			if source == workSourceRecover {
				recoveryClaims.Add(1)
			}
			return claim(ids, limit)
		}, store
	}
	e.recovery[h.Name] = []task{{ID: 1, OwnerGeneration: 7}, {ID: 2, OwnerGeneration: 4}}
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); e.runScheduler() }()
	// The loop is already alive while recovery is pending. Limit one keeps
	// the second recovery queued, then completion wakes and refills it.
	require.Equal(t, TaskID(1), receiveAdmission(t, impl.entered))
	close(finish)
	require.Equal(t, TaskID(2), receiveAdmission(t, impl.entered))
	cancel()
	receiveAdmission(t, loopDone)
	require.Equal(t, int32(2), recoveryClaims.Load())
	for _, a := range h.admissions {
		receiveAdmission(t, a.workerDone)
	}
}

// This runs the production event loop, handler, resource limiter and Do-entry
// path. Only persistence/Do payload are doubles; it is not SQL lock evidence.
func TestAdmissionR3OtherTaskAndEventsProgress(t *testing.T) {
	for _, phase := range []string{"preparation", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			e, cancel := newAdmissionEngine(t, 4)
			blocked, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			base := newMemoryAttemptStore()
			store := admissionPhaseStore{taskAttemptStore: base}
			if phase == "preparation" {
				store.prepareFn = func(ctx context.Context, id TaskID, token string) error {
					close(blocked)
					<-release
					return base.prepare(ctx, id, token)
				}
			} else {
				store.prepareFn = func(context.Context, TaskID, string) error { return errors.New("injected prepare failure") }
				store.releaseFn = func(ctx context.Context, id TaskID) error {
					close(blocked)
					<-release
					return base.releaseUnstarted(ctx, id)
				}
			}
			finish := make(chan struct{})
			a, slow := addAdmissionHandler(e, "Slow", taskhelp.Max(2), store, finish)
			_, fast := addAdmissionHandler(e, "Fast", taskhelp.Max(2), newMemoryAttemptStore(), finish)
			var eligible atomic.Bool
			fast.accept = func(ids []TaskID) ([]TaskID, error) {
				if !eligible.Load() {
					return nil, nil
				}
				return ids, nil
			}
			ts, urgent := addAdmissionHandler(e, "Urgent", taskhelp.Max(1), newMemoryAttemptStore(), finish)
			ts.TimeSensitive = true
			loopDone := make(chan struct{})
			go func() { defer close(loopDone); e.runScheduler() }()
			t.Cleanup(func() {
				unblock()
				close(finish)
				cancel()
				receiveAdmission(t, loopDone)
				for _, h := range e.handlers {
					for _, a := range h.admissions {
						receiveAdmission(t, a.workerDone)
					}
				}
			})
			e.schedulerChannel <- schedulerEvent{Source: schedulerSourceDBPoll, DBTasks: map[string][]task{"Slow": {{ID: 1}}, "Fast": {{ID: 2}}}}
			receiveAdmission(t, blocked)
			if phase == "preparation" {
				require.Equal(t, 1, a.Max.ActiveThis())
			} else {
				require.Equal(t, 0, a.Max.ActiveThis())
			}
			eligible.Store(true)
			// Without the completion event, the previously refused ready row has
			// no new DB snapshot. It must be reconsidered while A is still blocked.
			e.schedulerChannel <- schedulerEvent{Source: schedulerSourceTaskCompleted, TaskType: "SyntheticPrior"}
			require.Equal(t, TaskID(2), receiveAdmission(t, fast.entered))
			e.schedulerChannel <- schedulerEvent{Source: schedulerSourcePeerNewTask, TaskType: "Urgent", TaskID: 3}
			require.Equal(t, TaskID(3), receiveAdmission(t, urgent.entered))
			select {
			case id := <-slow.entered:
				t.Fatalf("blocked A entered Do: %d", id)
			default:
			}
			unblock()
			if phase == "preparation" {
				require.Equal(t, TaskID(1), receiveAdmission(t, slow.entered))
			}
		})
	}
}

// Legacy admission fixtures now drive the real ready-result consumer and join
// workers before inspecting their state. No new eligibility algorithm lives here.
func settleAdmissions(t *testing.T, h *taskTypeHandler) {
	t.Helper()
	entries := make([]*taskAdmission, 0, len(h.admissions))
	for _, a := range h.admissions {
		entries = append(entries, a)
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		h.drainAdmissions()
		all := true
		for _, a := range entries {
			select {
			case <-a.workerDone:
			default:
				all = false
			}
		}
		if all {
			h.drainAdmissions()
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("admission workers did not settle")
		case <-tick.C:
		}
	}
}
