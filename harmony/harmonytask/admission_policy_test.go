package harmonytask

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestAdmissionCordonPreservesExplicitSchedulingOverride(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 2)
	defer cancel()
	finish := make(chan struct{})
	defer close(finish)
	h, impl := addAdmissionHandler(e, "Finalize", taskhelp.Max(1), newMemoryAttemptStore(), finish)
	h.SchedulingOverrides = map[string]bool{"Batch": true}
	_, _ = addAdmissionHandler(e, "Batch", taskhelp.Max(1), newMemoryAttemptStore(), finish)
	e.atomics.yieldBackground.Store(true)
	available := map[string]*taskSchedule{"Finalize": {hasID: map[TaskID]task{1: {ID: 1}}}, "Batch": {hasID: map[TaskID]task{2: {ID: 2}}}}
	ee := eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel, availableTasks: available}
	require.NoError(t, e.pollerTryAllWork(taskSourceLocal{available}, ee))
	a := h.admissions[1]
	require.NotNil(t, a)
	require.Equal(t, workSourceOverride, a.from)
	settleAdmissions(t, h)
	require.Equal(t, TaskID(1), receiveAdmission(t, impl.entered))
	// A stale pending-only preemption plan cannot cancel a task after entry.
	e.executePreemption(&preemptionPlan{candidates: []preemptCandidate{{handler: h, taskID: 1, pending: true, handle: a.handle}}})
	require.False(t, a.handle.IsPreempted())
	require.Equal(t, 1, h.Max.Active())
}

func TestAdmissionStoppedLoopDoesNotBlockEventDelivery(t *testing.T) {
	e, cancel := newAdmissionEngine(t, 1)
	for i := 0; i < cap(e.schedulerChannel); i++ {
		e.schedulerChannel <- schedulerEvent{}
	}
	cancel()
	ee := eventEmitter{ctx: e.cfg.ctx, schedulerChannel: e.schedulerChannel}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ee.EmitTaskCompleted("Stopped", false)
		ee.EmitTaskStarted("Stopped", 1)
		ee.EmitTaskNew("Stopped", task{ID: 1})
	}()
	receiveAdmission(t, done)
	collect, _ := bundler(e.cfg.ctx)
	collect("Stopped")
	// Timer delivery uses the same cancellation boundary, with no blocked
	// result send after the event loop exits.
}
