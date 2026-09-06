package storageingest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewSealWakeRequestsStartupDrain(t *testing.T) {
	wake := newSealWake()
	select {
	case <-wake:
	default:
		t.Fatal("startup drain was not requested")
	}
	select {
	case <-wake:
		t.Fatal("more than one startup drain was requested")
	default:
	}
}

func TestSealLoopDoesNotRunQueuedWakeAfterCancellation(t *testing.T) {
	for range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		wake := make(chan struct{}, 1)
		wake <- struct{}{}
		cancel()

		calls := 0
		runSealLoop(ctx, make(chan time.Time), wake, func() error {
			calls++
			return nil
		}, func(err error) {
			t.Errorf("unexpected seal error: %v", err)
		})
		if calls != 0 {
			t.Fatalf("seal calls after cancellation = %d, want 0", calls)
		}
	}
}

func TestSealWakeCoalescesAndDoesNotOverlap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticks := make(chan time.Time)
	wake := make(chan struct{}, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	secondDone := make(chan struct{})
	loopDone := make(chan struct{})

	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	go func() {
		defer close(loopDone)
		runSealLoop(ctx, ticks, wake, func() error {
			call := calls.Add(1)
			current := active.Add(1)
			for {
				previous := maxActive.Load()
				if current <= previous || maxActive.CompareAndSwap(previous, current) {
					break
				}
			}
			defer active.Add(-1)

			switch call {
			case 1:
				close(started)
				<-release
			case 2:
				close(secondDone)
			}
			return nil
		}, func(err error) {
			t.Errorf("unexpected seal error: %v", err)
		})
	}()

	wakeSealLoop(wake)
	waitForSealTestSignal(t, started, "first seal pass")
	for range 100 {
		wakeSealLoop(wake)
	}
	close(release)
	waitForSealTestSignal(t, secondDone, "coalesced seal pass")
	cancel()
	waitForSealTestSignal(t, loopDone, "seal loop shutdown")

	if got := calls.Load(); got != 2 {
		t.Fatalf("seal calls = %d, want 2", got)
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent seal calls = %d, want 1", got)
	}
}

func TestSealLoopKeepsTickerFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticks := make(chan time.Time, 1)
	wake := make(chan struct{}, 1)
	called := make(chan struct{})
	go runSealLoop(ctx, ticks, wake, func() error {
		close(called)
		return nil
	}, func(err error) {
		t.Errorf("unexpected seal error: %v", err)
	})

	ticks <- time.Now()
	waitForSealTestSignal(t, called, "ticker seal pass")
}

func TestPieceIngesterWakeIsNonBlocking(t *testing.T) {
	p := &PieceIngester{sealWake: make(chan struct{}, 1)}
	for range 100 {
		p.Wake()
	}

	select {
	case <-p.sealWake:
	default:
		t.Fatal("Wake did not queue a seal pass")
	}
	select {
	case <-p.sealWake:
		t.Fatal("Wake queued more than one seal pass")
	default:
	}
}

func TestPieceIngesterSnapWakeIsNonBlocking(t *testing.T) {
	p := &PieceIngesterSnap{sealWake: make(chan struct{}, 1)}
	for range 100 {
		p.Wake()
	}

	select {
	case <-p.sealWake:
	default:
		t.Fatal("Wake did not queue a seal pass")
	}
	select {
	case <-p.sealWake:
		t.Fatal("Wake queued more than one seal pass")
	default:
	}
}

func TestSortedProviderIDsUsesStableLockOrder(t *testing.T) {
	details := map[int64]*mdetails{
		1003: {},
		1001: {},
		1002: {},
	}

	got := sortedProviderIDs(details)
	want := []int64{1001, 1002, 1003}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("provider order = %v, want %v", got, want)
		}
	}
}

func waitForSealTestSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
