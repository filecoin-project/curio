package harmonytask

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/preemptbids"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
)

func newAcceptanceTestEngine(t *testing.T, tasks ...*stubAcceptTask) *TaskEngine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := &TaskEngine{
		cfg: taskEngineConfig{
			ctx:   ctx,
			grace: cancel,
			reg:   &resources.Reg{Resources: resources.Resources{Cpu: 2, Ram: 1 << 30}},
		},
		taskMap:          make(map[string]*taskTypeHandler),
		schedulerChannel: make(chan schedulerEvent, len(tasks)*2),
	}
	for i, task := range tasks {
		h := &taskTypeHandler{
			TaskInterface:   task,
			TaskTypeDetails: task.TypeDetails(),
			TaskEngine:      e,
			accept:          acceptcache.New(time.Hour),
			running:         runregistry.New(),
		}
		h.Name = fmt.Sprintf("Accept%d", i)
		e.handlers = append(e.handlers, h)
		e.taskMap[h.Name] = h
	}
	e.startAcceptWorkers()
	return e
}

func acceptanceTestGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return gate, release
}

func waitAcceptanceTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for acceptance worker")
		var zero T
		return zero
	}
}

func applyAcceptanceTestEvent(t *testing.T, e *TaskEngine, event schedulerEvent) {
	t.Helper()
	require.Equal(t, schedulerSourceAcceptResult, event.Source)
	require.NotNil(t, event.Acceptance)
	h := e.taskMap[event.TaskType]
	require.NotNil(t, h)
	h.awaitingCanAcceptResult = false
	h.storeAcceptance(*event.Acceptance)
}

func TestAcceptWorkersBoundConcurrentChecksAcrossTypes(t *testing.T) {
	for _, timeSensitive := range []bool{false, true} {
		t.Run(fmt.Sprintf("time_sensitive=%t", timeSensitive), func(t *testing.T) {
			const taskCount = ACCEPT_WORKERS * 8
			gate, release := acceptanceTestGate(t)
			started := make(chan struct{}, taskCount)
			var active, peak atomic.Int32
			tasks := make([]*stubAcceptTask, taskCount)
			for i := range tasks {
				tasks[i] = &stubAcceptTask{timeSensitive: timeSensitive, accept: func(ids []TaskID) ([]TaskID, error) {
					current := active.Add(1)
					for previous := peak.Load(); current > previous; previous = peak.Load() {
						if peak.CompareAndSwap(previous, current) {
							break
						}
					}
					started <- struct{}{}
					<-gate
					active.Add(-1)
					return ids, nil
				}}
			}
			e := newAcceptanceTestEngine(t, tasks...)
			for i, h := range e.handlers {
				_, ready := h.resolveAcceptedIDs([]TaskID{TaskID(i + 1)})
				require.False(t, ready)
			}
			for range ACCEPT_WORKERS {
				waitAcceptanceTestValue(t, started)
			}
			require.Equal(t, int32(ACCEPT_WORKERS), active.Load())
			release()
			completed := make(map[string]bool, taskCount)
			for range taskCount {
				event := waitAcceptanceTestValue(t, e.schedulerChannel)
				require.False(t, completed[event.TaskType], "duplicate acceptance check")
				completed[event.TaskType] = true
				applyAcceptanceTestEvent(t, e, event)
			}
			require.Equal(t, int32(ACCEPT_WORKERS), peak.Load())
			for _, task := range tasks {
				require.Equal(t, int32(1), task.canAcceptCalls.Load())
			}
		})
	}
}

func TestAcceptWorkersDeduplicateQueuedRunningAndUnhandledChecks(t *testing.T) {
	for _, timeSensitive := range []bool{false, true} {
		t.Run(fmt.Sprintf("time_sensitive=%t", timeSensitive), func(t *testing.T) {
			blockersGate, releaseBlockers := acceptanceTestGate(t)
			targetGate, releaseTarget := acceptanceTestGate(t)
			blockersStarted := make(chan struct{}, ACCEPT_WORKERS)
			targetStarted := make(chan struct{}, 2)
			tasks := make([]*stubAcceptTask, 0, ACCEPT_WORKERS+1)
			for range ACCEPT_WORKERS {
				tasks = append(tasks, &stubAcceptTask{timeSensitive: timeSensitive, accept: func(ids []TaskID) ([]TaskID, error) {
					blockersStarted <- struct{}{}
					<-blockersGate
					return ids, nil
				}})
			}
			target := &stubAcceptTask{timeSensitive: timeSensitive, accept: func(ids []TaskID) ([]TaskID, error) {
				targetStarted <- struct{}{}
				<-targetGate
				return ids, nil
			}}
			tasks = append(tasks, target)
			e := newAcceptanceTestEngine(t, tasks...)
			queue := e.acceptWork
			if timeSensitive {
				queue = e.acceptSensitiveWork
			}
			for _, h := range e.handlers[:ACCEPT_WORKERS] {
				h.resolveAcceptedIDs([]TaskID{1})
			}
			for range ACCEPT_WORKERS {
				waitAcceptanceTestValue(t, blockersStarted)
			}
			h := e.handlers[ACCEPT_WORKERS]
			requestRepeatedly := func() {
				for range 50 {
					accepted, ready := h.resolveAcceptedIDs([]TaskID{9})
					require.False(t, ready)
					require.Empty(t, accepted)
				}
			}
			requestRepeatedly()
			require.Len(t, queue, 1, "only one queued check for the type")
			require.Zero(t, target.canAcceptCalls.Load())
			releaseBlockers()
			waitAcceptanceTestValue(t, targetStarted)
			requestRepeatedly()
			require.Empty(t, queue)
			require.Equal(t, int32(1), target.canAcceptCalls.Load())
			releaseTarget()
			var result schedulerEvent
			for {
				event := waitAcceptanceTestValue(t, e.schedulerChannel)
				if event.TaskType == h.Name {
					result = event
					break
				}
			}
			requestRepeatedly()
			require.Empty(t, queue, "a result awaiting admission still suppresses checks")
			require.Equal(t, int32(1), target.canAcceptCalls.Load())
			applyAcceptanceTestEvent(t, e, result)
			accepted, ready := h.resolveAcceptedIDs([]TaskID{9})
			require.True(t, ready)
			require.Equal(t, []TaskID{9}, accepted)
			require.Equal(t, int32(1), target.canAcceptCalls.Load())
		})
	}
}

func TestAcceptWorkerPoolsRunIndependently(t *testing.T) {
	for _, blockedSensitive := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked_time_sensitive=%t", blockedSensitive), func(t *testing.T) {
			const blockedCount = ACCEPT_WORKERS + 1
			blockedGate, releaseBlocked := acceptanceTestGate(t)
			otherGate, releaseOther := acceptanceTestGate(t)
			blockedStarted := make(chan struct{}, blockedCount)
			otherStarted := make(chan struct{}, ACCEPT_WORKERS)
			tasks := make([]*stubAcceptTask, 0, blockedCount+ACCEPT_WORKERS)
			for range blockedCount {
				tasks = append(tasks, &stubAcceptTask{timeSensitive: blockedSensitive, accept: func(ids []TaskID) ([]TaskID, error) {
					blockedStarted <- struct{}{}
					<-blockedGate
					return ids, nil
				}})
			}
			for range ACCEPT_WORKERS {
				tasks = append(tasks, &stubAcceptTask{timeSensitive: !blockedSensitive, accept: func(ids []TaskID) ([]TaskID, error) {
					otherStarted <- struct{}{}
					<-otherGate
					return ids, nil
				}})
			}
			e := newAcceptanceTestEngine(t, tasks...)
			for i, h := range e.handlers[:blockedCount] {
				h.resolveAcceptedIDs([]TaskID{TaskID(i + 1)})
			}
			for range ACCEPT_WORKERS {
				waitAcceptanceTestValue(t, blockedStarted)
			}
			require.Zero(t, tasks[blockedCount-1].canAcceptCalls.Load(), "the saturated pool must leave its fifth check queued")

			for i, h := range e.handlers[blockedCount:] {
				h.resolveAcceptedIDs([]TaskID{TaskID(blockedCount + i + 1)})
			}
			for range ACCEPT_WORKERS {
				waitAcceptanceTestValue(t, otherStarted)
			}
			releaseOther()
			completed := make(map[string]bool, len(tasks))
			for range ACCEPT_WORKERS {
				event := waitAcceptanceTestValue(t, e.schedulerChannel)
				require.NotEqual(t, blockedSensitive, e.taskMap[event.TaskType].TimeSensitive)
				require.False(t, completed[event.TaskType], "duplicate acceptance result")
				completed[event.TaskType] = true
				applyAcceptanceTestEvent(t, e, event)
				accepted, ready := e.taskMap[event.TaskType].resolveAcceptedIDs(event.Acceptance.ids)
				require.True(t, ready)
				require.Equal(t, event.Acceptance.ids, accepted)
			}
			require.Zero(t, tasks[blockedCount-1].canAcceptCalls.Load(), "the other pool must not take the queued check")

			releaseBlocked()
			for range blockedCount {
				event := waitAcceptanceTestValue(t, e.schedulerChannel)
				require.Equal(t, blockedSensitive, e.taskMap[event.TaskType].TimeSensitive)
				require.False(t, completed[event.TaskType], "duplicate acceptance result")
				completed[event.TaskType] = true
				applyAcceptanceTestEvent(t, e, event)
			}
			for _, task := range tasks {
				require.Equal(t, int32(1), task.canAcceptCalls.Load())
			}
		})
	}
}

func TestAcceptWorkersSlowTypeDoesNotBlockFastType(t *testing.T) {
	gate, _ := acceptanceTestGate(t)
	slowStarted := make(chan struct{}, 1)
	slow := &stubAcceptTask{accept: func(ids []TaskID) ([]TaskID, error) {
		slowStarted <- struct{}{}
		<-gate
		return ids, nil
	}}
	fast := &stubAcceptTask{}
	e := newAcceptanceTestEngine(t, slow, fast)
	e.handlers[0].resolveAcceptedIDs([]TaskID{1})
	waitAcceptanceTestValue(t, slowStarted)
	e.handlers[1].resolveAcceptedIDs([]TaskID{2})
	event := waitAcceptanceTestValue(t, e.schedulerChannel)
	require.Equal(t, e.handlers[1].Name, event.TaskType)
	applyAcceptanceTestEvent(t, e, event)
	accepted, ready := e.handlers[1].resolveAcceptedIDs([]TaskID{2})
	require.True(t, ready)
	require.Equal(t, []TaskID{2}, accepted)
}

func TestAcceptWorkersCacheRefusalAndErrorUntilRefresh(t *testing.T) {
	for _, checkError := range []error{nil, errors.New("acceptance unavailable")} {
		name := "refusal"
		if checkError != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			stub := &stubAcceptTask{accept: func(ids []TaskID) ([]TaskID, error) {
				if calls.Add(1) == 1 {
					if checkError != nil {
						return ids, checkError
					}
					return nil, nil
				}
				return ids, nil
			}}
			e := newAcceptanceTestEngine(t, stub)
			h := e.handlers[0]
			_, ready := h.resolveAcceptedIDs([]TaskID{1})
			require.False(t, ready)
			applyAcceptanceTestEvent(t, e, waitAcceptanceTestValue(t, e.schedulerChannel))
			for range 50 {
				accepted, ready := h.resolveAcceptedIDs([]TaskID{1})
				require.True(t, ready)
				require.Empty(t, accepted)
			}
			require.Empty(t, e.acceptWork)
			require.Equal(t, int32(1), stub.canAcceptCalls.Load())
			h.accept.Refresh()
			_, ready = h.resolveAcceptedIDs([]TaskID{1})
			require.False(t, ready)
			applyAcceptanceTestEvent(t, e, waitAcceptanceTestValue(t, e.schedulerChannel))
			accepted, ready := h.resolveAcceptedIDs([]TaskID{1})
			require.True(t, ready)
			require.Equal(t, []TaskID{1}, accepted)
			require.Equal(t, int32(2), stub.canAcceptCalls.Load())
		})
	}
}

func TestAcceptWorkersRecoveryReusesCheckedAcceptance(t *testing.T) {
	stub := &stubAcceptTask{}
	e := newAcceptanceTestEngine(t, stub)
	h := e.handlers[0]
	recovered := make(chan error, 1)
	go func() {
		recovered <- e.checkRecoveredTask(h, 7)
	}()
	require.NoError(t, waitAcceptanceTestValue(t, recovered))
	accepted, ready := h.resolveAcceptedIDs([]TaskID{7})
	require.True(t, ready)
	require.Equal(t, []TaskID{7}, accepted)
	require.Equal(t, int32(1), stub.canAcceptCalls.Load())
	require.Empty(t, e.acceptWork)
	require.Empty(t, e.schedulerChannel, "recovery must finish before the scheduler starts")
}

func TestSchedulerHandlesDiscoveryAndCompletionWhileAcceptanceBlocks(t *testing.T) {
	gate, _ := acceptanceTestGate(t)
	slowStarted := make(chan struct{}, 1)
	fastChecked := make(chan struct{}, 2)
	fastResultHandled := make(chan struct{}, 1)
	slow := &stubAcceptTask{accept: func([]TaskID) ([]TaskID, error) {
		slowStarted <- struct{}{}
		<-gate
		return nil, nil
	}}
	fast := &stubAcceptTask{accept: func([]TaskID) ([]TaskID, error) {
		fastChecked <- struct{}{}
		return nil, nil
	}}
	e := newAcceptanceTestEngine(t, slow, fast)
	e.cfg.reg = &resources.Reg{Resources: resources.Resources{Cpu: 2, Ram: 1 << 30}}
	slowHandler, fastHandler := e.handlers[0], e.handlers[1]
	fastHandler.IAmBored = func(AddTaskFunc) error {
		if _, checked := fastHandler.accept.GetMatching([]int64{2}); checked {
			select {
			case fastResultHandled <- struct{}{}:
			default:
			}
		}
		return nil
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.runScheduler()
	}()
	t.Cleanup(func() {
		e.cfg.grace()
		waitAcceptanceTestValue(t, stopped)
	})

	e.schedulerChannel <- schedulerEvent{
		Source: schedulerSourceDBPoll,
		DBTasks: map[string][]task{
			slowHandler.Name: {{ID: 1}},
			fastHandler.Name: nil,
		},
	}
	waitAcceptanceTestValue(t, slowStarted)
	e.schedulerChannel <- schedulerEvent{
		Source: schedulerSourceDBPoll,
		DBTasks: map[string][]task{
			slowHandler.Name: {{ID: 1}},
			fastHandler.Name: {{ID: 2}},
		},
	}
	waitAcceptanceTestValue(t, fastChecked)
	waitAcceptanceTestValue(t, fastResultHandled)
	e.schedulerChannel <- schedulerEvent{
		Source:   schedulerSourceTaskCompleted,
		TaskType: fastHandler.Name,
		TaskID:   99,
	}
	waitAcceptanceTestValue(t, fastChecked)
	require.Equal(t, int32(1), slow.canAcceptCalls.Load())
	require.Equal(t, int32(2), fast.canAcceptCalls.Load())
}

func TestSchedulerAcceptanceResultContinuesTimeSensitivePreemption(t *testing.T) {
	gate, release := acceptanceTestGate(t)
	checking := make(chan struct{}, 1)
	preempted := make(chan struct{})
	victim := &stubAcceptTask{}
	urgent := &stubAcceptTask{timeSensitive: true, accept: func(ids []TaskID) ([]TaskID, error) {
		checking <- struct{}{}
		<-gate
		return ids, nil
	}}
	e := newAcceptanceTestEngine(t, victim, urgent)
	e.cfg.reg = &resources.Reg{Resources: resources.Resources{Cpu: 1, Ram: 1 << 30}}
	e.state.preemptBids = preemptbids.New()
	e.peering = &peering{h: e, peers: peerregistry.New()}
	victimHandler, urgentHandler := e.handlers[0], e.handlers[1]
	victimHandler.Cost.Cpu = 1
	victimHandler.Max.Add(1)
	victimHandler.running.Start(1, func() { close(preempted) })
	urgentHandler.Cost.Cpu = 1
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		e.runScheduler()
	}()
	t.Cleanup(func() {
		e.cfg.grace()
		waitAcceptanceTestValue(t, stopped)
	})

	e.schedulerChannel <- schedulerEvent{
		Source:   schedulerSourcePeerNewTask,
		TaskType: urgentHandler.Name,
		TaskID:   2,
	}
	waitAcceptanceTestValue(t, checking)
	select {
	case <-preempted:
		t.Fatal("preempted a task before acceptance completed")
	default:
	}
	release()
	waitAcceptanceTestValue(t, preempted)
	require.Equal(t, int32(1), urgent.canAcceptCalls.Load(), "preemption must reuse the worker result")
}
