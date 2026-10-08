package openpieces

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitUntilZero(t *testing.T) {
	ctx := context.Background()

	t.Run("returns once the row is gone", func(t *testing.T) {
		var polls atomic.Int32
		err := waitUntilZero(ctx, time.Minute, []int64{1}, func(context.Context) (int, error) {
			if polls.Add(1) < 3 {
				return 1, nil
			}
			return 0, nil
		})
		require.NoError(t, err)
		require.Equal(t, int32(3), polls.Load())
	})

	t.Run("no queued row returns at once", func(t *testing.T) {
		var polls atomic.Int32
		require.NoError(t, waitUntilZero(ctx, time.Minute, []int64{1}, func(context.Context) (int, error) {
			polls.Add(1)
			return 0, nil
		}))
		require.Equal(t, int32(1), polls.Load())
	})

	t.Run("a stuck row returns nil after the bound", func(t *testing.T) {
		start := time.Now()
		err := waitUntilZero(ctx, 300*time.Millisecond, []int64{1}, func(context.Context) (int, error) { return 1, nil })
		require.NoError(t, err)
		require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
		require.Less(t, time.Since(start), 5*time.Second)
	})

	t.Run("cancelled ctx is returned", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := waitUntilZero(cctx, time.Minute, []int64{1}, func(context.Context) (int, error) { return 1, nil })
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("query errors are returned", func(t *testing.T) {
		boom := errors.New("boom")
		err := waitUntilZero(ctx, time.Minute, []int64{1}, func(context.Context) (int, error) { return 0, boom })
		require.ErrorIs(t, err, boom)
	})
}

func TestWaitPlacedNoRefs(t *testing.T) {
	require.NoError(t, WaitPlacedFor(context.Background(), nil, time.Minute))
}
