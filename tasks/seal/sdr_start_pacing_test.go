package seal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSDRStartPacing(t *testing.T) {
	now := sdrPacingTime{wall: time.Unix(100, 0)}
	p, err := newSDRStartPacer(time.Minute, false, "", func() sdrPacingTime { return now })
	require.NoError(t, err)
	a, ok := p.reserve()
	require.True(t, ok)
	_, ok = p.reserve()
	require.False(t, ok)
	p.cancel(a)
	b, ok := p.reserve()
	require.True(t, ok)
	p.cancel(a) // stale cleanup cannot release the new reservation
	_, ok = p.reserve()
	require.False(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.start(ctx, b), context.Canceled)
	c, ok := p.reserve()
	require.True(t, ok)
	require.NoError(t, p.start(context.Background(), c))
	p.cancel(c) // a failure after Do entry cannot refund spacing
	_, ok = p.reserve()
	require.False(t, ok)
	now.elapsed = time.Minute
	now.wall = now.wall.Add(-time.Hour) // wall-clock adjustment does not extend spacing
	_, ok = p.reserve()
	require.True(t, ok)

	disabled, err := newSDRStartPacer(0, true, "", nil)
	require.NoError(t, err)
	require.Nil(t, disabled)
	_, err = newSDRStartPacer(-1, false, "", nil)
	require.Error(t, err)
	_, err = newSDRStartPacer(time.Minute, true, "", nil)
	require.Error(t, err)
}

func TestSDRPhaseLatchedAndConcurrentReservation(t *testing.T) {
	now := sdrPacingTime{wall: time.Unix(100, 0)}
	p, err := newSDRStartPacer(time.Minute, true, "instance", func() sdrPacingTime { return now })
	require.NoError(t, err)
	_, _ = p.reserve()
	wait := p.phaseWait
	require.Positive(t, wait)
	for i := 0; i < 10; i++ {
		now.wall = now.wall.Add(time.Hour)
		_, ok := p.reserve()
		require.False(t, ok)
		require.Equal(t, wait, p.phaseWait)
	}
	now.elapsed = wait
	var wg sync.WaitGroup
	winners := make(chan uint64, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if token, ok := p.reserve(); ok {
				winners <- token
			}
		}()
	}
	wg.Wait()
	require.Len(t, winners, 1)
	require.NoError(t, p.start(context.Background(), <-winners))
}
