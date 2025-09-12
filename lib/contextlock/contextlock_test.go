package contextlock

import (
	"context"
	"testing"
	"time"
)

// TestContextLockBasic verifies that a lock can be taken and released once
// and that refcounts are handled correctly.
func TestContextLockBasic(t *testing.T) {
	l := NewContextLock()
	ctx := context.Background()

	// First lock should set refcount to 1
	ctx = l.Lock(ctx)
	if v := ctx.Value(l.lockContextKey); v == nil || v.(int) != 1 {
		t.Fatalf("expected refcount 1 after first lock, got %v", v)
	}

	// Unlock should remove the refcount and release the lock
	ctx = l.Unlock(ctx)
	if v := ctx.Value(l.lockContextKey); v != nil {
		t.Fatalf("expected nil refcount after unlock, got %v", v)
	}

	// Lock again should behave like the first lock
	ctx = l.Lock(ctx)
	if v := ctx.Value(l.lockContextKey); v == nil || v.(int) != 1 {
		t.Fatalf("expected refcount 1 after relock, got %v", v)
	}

	// Cleanup
	_ = l.Unlock(ctx)
}

// TestContextLockReentrant ensures that taking the lock twice increments the refcount
// and requires the same number of unlocks to release it.
func TestContextLockReentrant(t *testing.T) {
	l := NewContextLock()
	ctx := context.Background()

	ctx = l.Lock(ctx) // refcount 1
	ctx = l.Lock(ctx) // refcount 2

	if v := ctx.Value(l.lockContextKey).(int); v != 2 {
		t.Fatalf("expected refcount 2 after second lock, got %v", v)
	}

	// One unlock should decrement refcount but keep the lock held
	ctx = l.Unlock(ctx)
	if v := ctx.Value(l.lockContextKey); v == nil || v.(int) != 1 {
		t.Fatalf("expected refcount 1 after first unlock, got %v", v)
	}

	// Second unlock should release completely
	ctx = l.Unlock(ctx)
	if v := ctx.Value(l.lockContextKey); v != nil {
		t.Fatalf("expected nil refcount after second unlock, got %v", v)
	}
}

// TestContextLockConcurrent verifies that a second goroutine blocks until
// the first goroutine releases the lock.
func TestContextLockConcurrent(t *testing.T) {
	l := NewContextLock()
	ctx := context.Background()

	// Take the lock in the main goroutine
	ctx = l.Lock(ctx)

	done := make(chan struct{})
	go func() {
		// Attempt to take the lock; this should block until main goroutine releases it
		lc := l.Lock(context.Background())
		// Release immediately after acquiring so the goroutine can finish
		l.Unlock(lc)
		close(done)
	}()

	// Ensure the goroutine is blocked by waiting a short time and verifying it's not done
	select {
	case <-done:
		t.Fatalf("lock acquired by second goroutine while it should be held")
	case <-time.After(10 * time.Millisecond):
		// Expected path; goroutine should still be blocked
	}

	// Release the lock; the goroutine should now finish quickly
	ctx = l.Unlock(ctx)
	if v := ctx.Value(l.lockContextKey); v != nil {
		t.Fatalf("expected refcount 0 after unlock, got %v", v)
	}

	select {
	case <-done:
		// success
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("second goroutine did not acquire lock after it was released")
	}
}

// TestContextLockUnlockWithoutLock ensures that calling Unlock on a context that
// never had the lock does not panic and returns the same context untouched.
func TestContextLockUnlockWithoutLock(t *testing.T) {
	l := NewContextLock()
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic when unlocking without prior lock: %v", r)
		}
	}()

	newCtx := l.Unlock(ctx)
	if newCtx != ctx {
		t.Fatalf("expected Unlock to return the original context when no lock was held")
	}
	if v := newCtx.Value(l.lockContextKey); v != nil {
		t.Fatalf("expected no lock value in context, got %v", v)
	}
}

// TestContextLockMultipleRefcounts checks high refcount increments and decrements.
func TestContextLockMultipleRefcounts(t *testing.T) {
	const n = 10
	l := NewContextLock()
	ctx := context.Background()

	// Increment refcount n times
	for i := 1; i <= n; i++ {
		ctx = l.Lock(ctx)
		if v := ctx.Value(l.lockContextKey); v == nil || v.(int) != i {
			t.Fatalf("expected refcount %d after lock %d, got %v", i, i, v)
		}
	}

	// Decrement refcount n times
	for i := n; i >= 1; i-- {
		ctx = l.Unlock(ctx)
		if i == 1 {
			if v := ctx.Value(l.lockContextKey); v != nil {
				t.Fatalf("expected nil refcount after final unlock, got %v", v)
			}
		} else {
			expected := i - 1
			if v := ctx.Value(l.lockContextKey); v == nil || v.(int) != expected {
				t.Fatalf("expected refcount %d after unlock, got %v", expected, v)
			}
		}
	}
}
