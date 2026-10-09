package seal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

func awaitSDREntryLog(t *testing.T, p *sdrStartPacer) {
	t.Helper()
	if p != nil {
		require.Eventually(t, func() bool { return !p.observer.entryLogBusy.Load() }, time.Second, time.Millisecond,
			"entry diagnostic did not finish")
	}
}

func TestSDRAdmissionDoesNotWaitForEntryLogger(t *testing.T) {
	var elapsed atomic.Int64
	p, err := newSDRStartPacer(time.Second, false, "synthetic", func() sdrPacingTime {
		return sdrPacingTime{elapsed: time.Duration(elapsed.Load()), wall: time.Unix(1800000000, 0)}
	})
	require.NoError(t, err)
	blocked, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	var logs atomic.Int64
	p.observer.sink = func(sdrPacingEvent) {
		logs.Add(1)
		close(blocked)
		<-release
		// Re-enter diagnostics after release: no pacer lock is held by logging.
		_ = p.snapshot()
		close(joined)
	}
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Error("entry logger did not join")
		}
	})
	s := &SDRTask{startPacer: p}
	commit := func(id int) {
		start, cancel, ok := s.ReserveTaskStart(harmonytask.TaskID(id))
		require.True(t, ok)
		done := make(chan error, 1)
		go func() { done <- start(context.Background()) }()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("entry commit waited for diagnostic sink")
		}
		cancel() // Cannot refund the committed interval.
	}
	commit(1)
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("logger did not reach barrier")
	}
	for id := 2; id <= 10; id++ {
		elapsed.Store(int64(id) * int64(time.Second))
		commit(id)
	}
	require.EqualValues(t, 1, logs.Load(), "one bounded outstanding entry diagnostic")
	before := p.snapshot()
	once.Do(func() { close(release) })
	<-joined
	require.Equal(t, before, p.snapshot(), "diagnostic completion cannot change pacing")
	require.True(t, p.started)
	require.Zero(t, p.reserved)
}
