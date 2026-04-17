package peerregistry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeConn struct {
	sent atomic.Int64
	err  error
}

func (f *fakeConn) SendMessage(_ []byte) error {
	f.sent.Add(1)
	return f.err
}

func TestAddBroadcast(t *testing.T) {
	r := New()
	a := &fakeConn{}
	b := &fakeConn{}
	_ = r.Add(1, "a:1", a, []string{"X", "Y"})
	_ = r.Add(2, "b:1", b, []string{"Y"})

	if !r.HasAny() {
		t.Fatal("expected HasAny true")
	}
	if got := r.CountFor("Y"); got != 2 {
		t.Fatalf("expected 2 peers for Y, got %d", got)
	}
	if got := r.CountFor("X"); got != 1 {
		t.Fatalf("expected 1 peer for X, got %d", got)
	}

	r.Broadcast("Y", []byte("m"), nil)
	waitFor(t, func() bool { return a.sent.Load() == 1 && b.sent.Load() == 1 })
}

func TestRemove(t *testing.T) {
	r := New()
	a := &fakeConn{}
	remove := r.Add(1, "a:1", a, []string{"X"})
	remove()
	remove() // second call is a no-op
	if r.HasAny() {
		t.Fatal("expected HasAny false after remove")
	}
	if got := r.CountFor("X"); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
	r.Broadcast("X", []byte("m"), nil)
	time.Sleep(20 * time.Millisecond)
	if got := a.sent.Load(); got != 0 {
		t.Fatalf("expected removed peer to get no message, got %d", got)
	}
}

func TestBroadcastErrorHook(t *testing.T) {
	r := New()
	a := &fakeConn{err: errFake{}}
	_ = r.Add(1, "a:1", a, []string{"X"})
	var mu sync.Mutex
	got := map[string]int{}
	r.Broadcast("X", []byte("m"), func(addr string, err error) {
		mu.Lock()
		got[addr]++
		mu.Unlock()
	})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got["a:1"] == 1
	})
}

func TestReconnectReplacesAndStaleRemoveIsNoop(t *testing.T) {
	r := New()
	first := &fakeConn{}
	second := &fakeConn{}

	removeFirst := r.Add(42, "peer:1", first, []string{"X"})
	_ = r.Add(42, "peer:2", second, []string{"X"})

	if got := r.CountFor("X"); got != 1 {
		t.Fatalf("expected 1 peer for X after reconnect (map-keyed by id), got %d", got)
	}

	removeFirst()

	if got := r.CountFor("X"); got != 1 {
		t.Fatalf("stale remove from previous session should be a no-op; got %d peers", got)
	}

	r.Broadcast("X", []byte("m"), nil)
	waitFor(t, func() bool { return second.sent.Load() == 1 })
	if got := first.sent.Load(); got != 0 {
		t.Fatalf("evicted connection should not receive broadcasts, got %d", got)
	}
}

type errFake struct{}

func (errFake) Error() string { return "fake" }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}
