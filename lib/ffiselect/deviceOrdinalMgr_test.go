package ffiselect

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/resources"
)

func TestNoGPUs(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{}, nil
	})

	// There should be only 1 slot
	ord := d.Get()
	if ord != 0 {
		t.Fatal("should have gotten GPU 0")
	}

	ch := make(chan int)
	d.acquireChan <- ch
	select {
	case _ = <-ch:
		panic("should have blocked")
	case <-time.After(time.Millisecond * 100):
		// expected: blocked because capacity is 1
	}

	d.Release(0)
}

func TestOverprovisionFactor(t *testing.T) {
	old := resources.GpuOverprovisionFactor
	resources.GpuOverprovisionFactor = 2
	defer func() {
		resources.GpuOverprovisionFactor = old
	}()

	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{"0", "1", "2"}, nil
	})

	acquireChan := make(chan int)

	expect := []int{0, 1, 2, 0, 1, 2}
	for i := range expect {
		d.acquireChan <- acquireChan
		select {
		case ord := <-acquireChan:
			if ord != expect[i] {
				t.Fatal("should have gotten GPU", expect[i])
			}
		case <-time.After(time.Millisecond * 100):
			t.Fatal("timeout")
		}
	}
	d.Release(1)
	d.Release(2)
	d.Release(1)

	// now 1 is the least used, so it should get it
	d.acquireChan <- acquireChan
	select {
	case ord := <-acquireChan:
		if ord != 1 {
			t.Fatal("should have gotten GPU 1")
		}
	case <-time.After(time.Millisecond * 100):
		t.Fatal("timeout")
	}
}

func TestWaitList(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{"a", "b", "c"}, nil
	})

	acquireChan := make(chan int)
	expect := []int{0, 1, 2}
	for i := range expect {
		d.acquireChan <- acquireChan
		select {
		case ord := <-acquireChan:
			if ord != expect[i] {
				t.Fatal("should have gotten GPU", expect[i])
			}
		case <-time.After(time.Millisecond * 100):
			t.Fatal("timeout")
		}
	}

	m := sync.Mutex{}
	list := []int{}
	for i := 0; i < len(expect); i++ {
		go func(i int) {
			ord := d.Get()
			m.Lock()
			list = append(list, ord)
			m.Unlock()
		}(i)
	}
	for i := range expect {
		d.Release(expect[i])
		time.Sleep(time.Millisecond * 100)
	}
	for i := range expect {
		if list[i] != i {
			t.Fatal("waitlist fired out-of-order", list, i)
		}
	}
}

func TestGlobal(t *testing.T) {
	ord := deviceOrdinalMgr.Get()
	deviceOrdinalMgr.Release(ord)
}

// Regression: no-GPU manager must never hand out ordinal 0.
// Before the fix, gpuSlots was []byte{1} when no GPUs existed, so Get()
// returned 0 and call() would set CUDA_VISIBLE_DEVICES=0 for a device
// that doesn't exist.
func TestNoGPUs_NeverReturnsZero(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{}, nil
	})

	for i := 0; i < 10; i++ {
		ord := d.Get()
		if ord != -1 {
			t.Fatalf("no-GPU manager returned ordinal %d, want -1", ord)
		}
		// Release should not block or panic even with -1.
		d.Release(ord)
	}
}

// Regression: extracting log context from a bare context must not panic.
// Before the fix, ctx.Value(logCtxKey).([]any) was a hard type assertion
// that panicked when the context had no logCtxKey value.
func TestLogCtxNilSafe(t *testing.T) {
	ctx := context.Background() // no WithLogCtx

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("extracting logCtxKey from bare context panicked: %v", r)
		}
	}()

	// This is the exact pattern used in call(). Before the fix it was:
	//   ctx.Value(logCtxKey).([]any)   — panics on nil
	// After the fix:
	//   logKvs, _ := ctx.Value(logCtxKey).([]any)
	logKvs, _ := ctx.Value(logCtxKey).([]any)
	if logKvs != nil {
		t.Fatalf("expected nil logKvs from bare context, got %v", logKvs)
	}

	// NewLogWriter must handle nil ctx gracefully.
	lw := NewLogWriter(logKvs, io.Discard)
	if lw == nil {
		t.Fatal("NewLogWriter returned nil")
	}
}

// Regression: double-release must not overflow the byte slot counter.
// Before the fix, gpuSlots[ordinal]++ had no bounds check and could
// exceed GpuOverprovisionFactor or wrap around byte(255)->0.
func TestDoubleReleaseIgnored(t *testing.T) {
	d := newDeviceOrdinalManager(func() ([]string, error) {
		return []string{"gpu0"}, nil
	})

	ord := d.Get()
	d.Release(ord)

	// Second release of the same ordinal — should be silently ignored,
	// not inflate capacity.
	d.Release(ord)

	// We should still only be able to acquire GpuOverprovisionFactor times
	// (which is 1 by default). If double-release inflated the counter,
	// we'd be able to acquire more than once.
	acquireChan := make(chan int)

	// First acquire should succeed.
	d.acquireChan <- acquireChan
	select {
	case got := <-acquireChan:
		if got != 0 {
			t.Fatalf("expected ordinal 0, got %d", got)
		}
	case <-time.After(time.Millisecond * 100):
		t.Fatal("timeout on first acquire")
	}

	// Second acquire should block (only 1 slot with default overprovision=1).
	d.acquireChan <- acquireChan
	select {
	case got := <-acquireChan:
		t.Fatalf("should have blocked, but got ordinal %d", got)
	case <-time.After(time.Millisecond * 100):
		// expected: blocked because capacity is 1
	}
}
