package runnowflags

import "testing"

func TestFlagReturnsSamePointer(t *testing.T) {
	r := New()
	a := r.Flag("x")
	b := r.Flag("x")
	if a != b {
		t.Fatal("expected identical pointer for same name")
	}
	a.Store(true)
	if !b.Load() {
		t.Fatal("expected Load to see Store via shared pointer")
	}
}

func TestSnapshotIndependent(t *testing.T) {
	r := New()
	r.Flag("a").Store(true)
	r.Flag("b")
	snap := r.Snapshot()
	if !snap["a"].Load() || snap["b"].Load() {
		t.Fatalf("unexpected snapshot state: %v/%v", snap["a"].Load(), snap["b"].Load())
	}
	if len(snap) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(snap))
	}
}
