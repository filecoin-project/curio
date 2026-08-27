package runregistry

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStartFinishRemovesEntry(t *testing.T) {
	r := New()
	_, cancel := context.WithCancel(context.Background())
	h := r.Start(7, cancel)
	if _, ok := r.Get(7); !ok {
		t.Fatal("expected task 7 in registry")
	}
	r.Finish(7)
	if _, ok := r.Get(7); ok {
		t.Fatal("expected task 7 removed after Finish")
	}
	select {
	case <-h.done:
	default:
		t.Fatal("Finish did not close done channel")
	}
}

func TestPreemptCancelsAndMarks(t *testing.T) {
	r := New()
	ctx, cancel := context.WithCancel(context.Background())
	h := r.Start(1, cancel)
	h.Preempt()
	if !h.IsPreempted() {
		t.Fatal("expected IsPreempted after Preempt")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected task context cancelled by Preempt")
	}
	r.Finish(1)
}

func TestSnapshotReflectsState(t *testing.T) {
	r := New()
	_, cancel := context.WithCancel(context.Background())
	h := r.Start(42, cancel)
	h.Preempt()
	entries := r.Snapshot()
	if len(entries) != 1 || entries[0].ID != 42 || !entries[0].Preempted {
		t.Fatalf("unexpected snapshot: %+v", entries)
	}
	r.Finish(42)
}

func TestWaitDoneDeadline(t *testing.T) {
	r := New()
	_, cancel := context.WithCancel(context.Background())
	h := r.Start(5, cancel)
	if ok := h.WaitDone(time.Now().Add(10 * time.Millisecond)); ok {
		t.Fatal("expected WaitDone to time out since Finish not called")
	}
	r.Finish(5)
}

func TestWaitDoneSharedDeadlineDoesNotHang(t *testing.T) {
	r := New()
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())
	h1 := r.Start(1, cancel1)
	h2 := r.Start(2, cancel2)

	deadline := time.Now().Add(20 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		_ = h1.WaitDone(deadline)
		_ = h2.WaitDone(deadline)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shared deadline WaitDone hung after first timeout")
	}
	r.Finish(1)
	r.Finish(2)
}

func TestConcurrentStartFinish(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := int64(0); i < 100; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			r.Start(id, cancel)
			r.Finish(id)
		}(i)
	}
	wg.Wait()
	if got := len(r.Snapshot()); got != 0 {
		t.Fatalf("expected empty registry, got %d entries", got)
	}
}
