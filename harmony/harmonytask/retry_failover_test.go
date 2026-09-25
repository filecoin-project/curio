package harmonytask

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
)

func TestRetryPeerKeepsCompletionClock(t *testing.T) {
	ch := make(chan schedulerEvent, 4)
	h := &taskTypeHandler{TaskTypeDetails: TaskTypeDetails{Name: "PoRep", RetryWait: func(int) time.Duration { return time.Millisecond }}, TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: context.Background()}}}
	h.emitRetryTask(eventEmitter{schedulerChannel: ch}, &task{ID: 91, Retries: 1, UpdateTime: time.Now().UTC(), PostedTime: time.Unix(100, 0)})
	var local schedulerEvent
	select {
	case local = <-ch:
	case <-time.After(time.Second):
		t.Fatal("local retry missing")
	}
	sender, receiver := pipePair()
	p := &peering{peers: peerregistry.New()}
	remove := p.peers.Add(2, "peer.example", sender, []string{"PoRep"})
	defer remove()
	p.TellNewTask(local.TaskType, local.TaskID, local.Retries, local.PostedTime, local.UpdateTime)
	var wire []byte
	select {
	case wire = <-receiver.recvCh:
	case <-time.After(time.Second):
		t.Fatal("wire retry missing")
	}
	dest := &peering{h: &TaskEngine{schedulerChannel: ch}}
	require.NoError(t, dest.handlePeerMessage("source.example", 1, wire))
	peer := taskFromSchedulerEvent(<-ch)
	source := taskFromSchedulerEvent(local)
	require.True(t, source.UpdateTime.Equal(peer.UpdateTime), "peer restarted the retry clock: local=%s peer=%s", source.UpdateTime, peer.UpdateTime)
	require.Equal(t, source.Retries, peer.Retries)
	require.Equal(t, retryDeadline(source, h.RetryWait), retryDeadline(peer, h.RetryWait))
	require.True(t, retryReady(peer, h.RetryWait, time.Now()))
}

func TestRetryDeadlinesOrderingAndLegacyMessages(t *testing.T) {
	now := time.Now().UTC()
	wait := func(n int) time.Duration { return min(time.Second<<n, 2*time.Minute) }
	for _, n := range []int{0, 1, 5, 9} {
		row := task{ID: 1, Retries: n, UpdateTime: now}
		deadline := retryDeadline(row, wait)
		if n == 0 {
			require.True(t, retryReady(row, wait, now))
			continue
		}
		require.False(t, retryReady(row, wait, deadline.Add(-time.Nanosecond)))
		require.True(t, retryReady(row, wait, deadline))
		s := &taskSchedule{hasID: map[TaskID]task{1: row}}
		all := map[string]*taskSchedule{"PoRep": s}
		hs := map[string]*taskTypeHandler{"PoRep": {TaskTypeDetails: TaskTypeDetails{RetryWait: wait}}}
		require.Equal(t, deadline, nextRetryDeadline(all, hs, now))
		require.True(t, nextRetryDeadline(all, hs, deadline).IsZero(), "elapsed deadline must not busy-loop")
		rememberTask(s, task{ID: 1, Retries: n - 1, UpdateTime: now.Add(time.Hour)})
		rememberTask(s, task{ID: 1, Retries: n, UpdateTime: now.Add(-time.Second)})
		rememberTask(s, row)
		require.Equal(t, row, s.hasID[1])
		forgetPeerStarted(s, schedulerEvent{TaskID: 1, Retries: n - 1, UpdateTime: now})
		forgetPeerStarted(s, schedulerEvent{TaskID: 1, Retries: n, UpdateTime: now.Add(-time.Second)})
		require.Equal(t, row, s.hasID[1], "delayed start must not erase a newer retry")
		// A legacy packet is conservatively timed from receipt, never immediate.
		legacy := taskFromSchedulerEvent(schedulerEvent{TaskID: 1, Retries: n})
		require.False(t, retryReady(legacy, wait, time.Now()))
		rememberTask(s, legacy)
		require.Equal(t, row, s.hasID[1])
		s.hasID[1] = legacy
		applyDBTaskSnapshot(all, map[string][]task{"PoRep": {row}})
		require.Equal(t, row, all["PoRep"].hasID[1], "DB must repair legacy receive-time fallback")
		// Connected peers share a deadline; missing packets are repaired by the
		// next 30s (or degraded 3s) DB poll, not by an invented host preference.
		for _, poll := range []time.Duration{30 * time.Second, 3 * time.Second} {
			observed := deadline.Add(poll)
			require.True(t, retryReady(all["PoRep"].hasID[1], wait, observed))
		}
	}
}

func TestRetryEmitterShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx}}, TaskTypeDetails: TaskTypeDetails{RetryWait: func(int) time.Duration { return time.Hour }}}
	ch := make(chan schedulerEvent, 1)
	h.emitRetryTask(eventEmitter{schedulerChannel: ch}, nil)
	h.emitRetryTask(eventEmitter{schedulerChannel: ch}, &task{ID: 1, Retries: 1, UpdateTime: time.Now()})
	select {
	case <-ch:
		t.Fatal("emitted after shutdown")
	case <-time.After(10 * time.Millisecond):
	}
}
