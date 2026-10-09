package taskhelp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkerBackoffReservationAndRecovery(t *testing.T) {
	now := time.Now()
	g := NewWorkerBackoff(func() time.Time { return now })
	start, cancel, ok := g.Reserve()
	require.True(t, ok)
	require.Nil(t, start)
	require.Nil(t, cancel)
	old := g.Epoch()
	failure := &WorkerUnavailable{Cause: errors.New("fixture unavailable")}
	require.Equal(t, 2*time.Minute, g.Result(old, failure))
	require.True(t, g.Blocked())
	g.Result(old, nil)
	require.True(t, g.Blocked(), "older success must not clear newer failure")
	now = now.Add(2 * time.Minute)
	start, cancel, ok = g.Reserve()
	require.True(t, ok)
	require.True(t, g.Blocked())
	ctx, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, start(ctx), context.Canceled)
	cancel()
	require.False(t, g.Blocked(), "pre-entry cancellation returns only its probe")
	_, second, ok := g.Reserve()
	require.True(t, ok)
	cancel()
	require.True(t, g.Blocked(), "old cleanup must not cancel newer probe")
	require.Equal(t, 4*time.Minute, g.Result(g.Epoch(), failure))
	second()
	require.True(t, g.Blocked())
	for i := 0; i < 8; i++ {
		now = now.Add(30 * time.Minute)
		_, end, accepted := g.Reserve()
		require.True(t, accepted)
		require.LessOrEqual(t, g.Result(g.Epoch(), failure), 30*time.Minute)
		end()
	}
	now = now.Add(30 * time.Minute)
	_, end, ok := g.Reserve()
	require.True(t, ok)
	g.Result(g.Epoch(), nil)
	end()
	require.False(t, g.Blocked())
	start, cancel, ok = g.Reserve()
	require.True(t, ok)
	require.Nil(t, start)
	require.Nil(t, cancel)
	require.False(t, new(WorkerBackoff).Blocked(), "restart loses process-local health history")
}

func TestWorkerBackoffConcurrentProbe(t *testing.T) {
	now := time.Now()
	g := NewWorkerBackoff(func() time.Time { return now })
	g.Result(0, &WorkerUnavailable{Cause: errors.New("fixture")})
	now = now.Add(2 * time.Minute)
	var wg sync.WaitGroup
	results := make(chan func(), 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cancel, ok := g.Reserve()
			if ok {
				results <- cancel
			}
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for cancel := range results {
		count++
		cancel()
	}
	require.Equal(t, 1, count)
}

func TestWorkerBackoffOrdinaryProbeAndLateResults(t *testing.T) {
	now := time.Now()
	g := NewWorkerBackoff(func() time.Time { return now })
	failure := &WorkerUnavailable{Cause: errors.New("fixture local C2 unavailable")}
	g.Result(g.Epoch(), failure)
	now = now.Add(2 * time.Minute)
	start, release, ok := g.Reserve()
	require.True(t, ok)
	require.NoError(t, start(context.Background()))
	old := g.Epoch()
	require.Equal(t, 2*time.Minute, g.Result(old, errors.New("ordinary invalid proof")), "ordinary probe errors hold but do not double backend delay")
	release()
	require.True(t, g.Blocked())
	now = now.Add(2 * time.Minute)
	_, second, ok := g.Reserve()
	require.True(t, ok)
	require.Equal(t, 4*time.Minute, g.Result(old, failure))
	g.Result(old, nil)
	release()
	require.True(t, g.Blocked(), "late success/cleanup cannot reopen the current probe")
	second()
	now = now.Add(4 * time.Minute)
	_, third, ok := g.Reserve()
	require.True(t, ok)
	g.Result(g.Epoch(), nil)
	third()
	require.False(t, g.Blocked())
	g.Result(old, failure)
	require.True(t, g.Blocked(), "a late observed backend failure still closes admission")
}
