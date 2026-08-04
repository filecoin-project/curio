package harmonytask

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

// stubAcceptTask implements TaskInterface for resolveAcceptedIDs / considerWork unit tests.
type stubAcceptTask struct {
	canAcceptCalls atomic.Int32
	accept         func([]TaskID) ([]TaskID, error)
}

func (s *stubAcceptTask) Do(context.Context, TaskID, func() bool) (bool, error) {
	return true, nil
}
func (s *stubAcceptTask) CanAccept(ids []TaskID, _ *TaskEngine) ([]TaskID, error) {
	s.canAcceptCalls.Add(1)
	if s.accept != nil {
		return s.accept(ids)
	}
	return ids, nil
}
func (s *stubAcceptTask) TypeDetails() TaskTypeDetails {
	return TaskTypeDetails{Name: "StubAccept", Max: taskhelp.Max(10)}
}
func (s *stubAcceptTask) Adder(AddTaskFunc) {}

func TestResolveAcceptedIDsCacheMissCallsLiveCanAccept(t *testing.T) {
	stub := &stubAcceptTask{}
	h := &taskTypeHandler{
		TaskInterface:   stub,
		TaskTypeDetails: stub.TypeDetails(),
		accept:          acceptcache.New(time.Hour),
	}
	// Cache has unrelated IDs — previous bug treated empty intersection as refuse.
	h.accept.Add([]int64{1, 2, 3})

	got, err := h.resolveAcceptedIDs([]TaskID{9, 10})
	require.NoError(t, err)
	require.Equal(t, []TaskID{9, 10}, got)
	require.Equal(t, int32(1), stub.canAcceptCalls.Load(), "live CanAccept required on cache miss")
	// Unmatched cache entries preserved for later.
	left, had := h.accept.TakeMatching([]int64{1, 2, 3})
	require.True(t, had)
	require.Equal(t, []int64{1, 2, 3}, left)
}

func TestResolveAcceptedIDsCacheHitSkipsCanAccept(t *testing.T) {
	stub := &stubAcceptTask{}
	h := &taskTypeHandler{
		TaskInterface:   stub,
		TaskTypeDetails: stub.TypeDetails(),
		accept:          acceptcache.New(time.Hour),
	}
	h.accept.Add([]int64{5, 6, 7})

	got, err := h.resolveAcceptedIDs([]TaskID{5, 6})
	require.NoError(t, err)
	require.Equal(t, []TaskID{5, 6}, got)
	require.Equal(t, int32(0), stub.canAcceptCalls.Load())
}

func TestResolveAcceptedIDsPartialHitLiveAcceptsMissing(t *testing.T) {
	stub := &stubAcceptTask{}
	h := &taskTypeHandler{
		TaskInterface:   stub,
		TaskTypeDetails: stub.TypeDetails(),
		accept:          acceptcache.New(time.Hour),
	}
	h.accept.Add([]int64{1, 2})

	got, err := h.resolveAcceptedIDs([]TaskID{1, 99})
	require.NoError(t, err)
	require.ElementsMatch(t, []TaskID{1, 99}, got)
	require.Equal(t, int32(1), stub.canAcceptCalls.Load())
}

func TestResolveAcceptedIDsEmptyCacheLiveCanAccept(t *testing.T) {
	stub := &stubAcceptTask{
		accept: func(ids []TaskID) ([]TaskID, error) { return nil, nil },
	}
	h := &taskTypeHandler{
		TaskInterface:   stub,
		TaskTypeDetails: stub.TypeDetails(),
		accept:          acceptcache.New(time.Hour),
	}

	got, err := h.resolveAcceptedIDs([]TaskID{1})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, int32(1), stub.canAcceptCalls.Load())
}

func TestConsiderWorkSkipsAlreadyRunning(t *testing.T) {
	stub := &stubAcceptTask{}
	h := &taskTypeHandler{
		TaskInterface:   stub,
		TaskTypeDetails: stub.TypeDetails(),
		running:         runregistry.New(),
		accept:          acceptcache.New(time.Hour),
		storageFailures: map[TaskID]time.Time{},
	}
	h.running.Start(42, func() {})

	// Would hit DB claim if not filtered — returning true means we treated it as handled.
	ok := h.considerWork(workSourcePoller, []task{{ID: 42}}, eventEmitter{})
	require.True(t, ok)
	require.Equal(t, int32(0), stub.canAcceptCalls.Load(), "must not CanAccept already-running tasks")
}

func TestNoteClaimedRemovesFromAvailable(t *testing.T) {
	avail := map[string]*taskSchedule{
		"Foo": {hasID: map[TaskID]task{1: {ID: 1}, 2: {ID: 2}}},
	}
	ee := eventEmitter{availableTasks: avail}
	ee.NoteClaimed("Foo", []TaskID{1})
	require.NotContains(t, avail["Foo"].hasID, TaskID(1))
	require.Contains(t, avail["Foo"].hasID, TaskID(2))

	ee.NoteClaimed("Missing", []TaskID{1}) // nil-safe
	ee = eventEmitter{}
	ee.NoteClaimed("Foo", []TaskID{2}) // nil availableTasks
}

func TestHandlePeerIdentitySendFailureDegradesPoll(t *testing.T) {
	engine := &TaskEngine{}
	engine.atomics.pollDuration.Store(pollRarely)
	p := &peering{h: engine, peers: peerregistry.New()}

	conn, _ := pipePair()
	require.NoError(t, conn.Close())

	p.handlePeer("unreachable:1", conn)
	require.Equal(t, pollFrequently, engine.atomics.pollDuration.Load().(time.Duration))
}
