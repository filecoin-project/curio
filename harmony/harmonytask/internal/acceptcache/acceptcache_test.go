package acceptcache

import (
	"reflect"
	"testing"
	"time"
)

func TestAddConsume(t *testing.T) {
	c := New(time.Hour)
	c.Add([]int64{1, 2, 3})
	got := c.Consume()
	if !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("unexpected ids: %v", got)
	}
	if got := c.Consume(); got != nil {
		t.Fatalf("expected nil on second consume, got %v", got)
	}
}

func TestTTLExpires(t *testing.T) {
	c := New(5 * time.Millisecond)
	c.Add([]int64{9})
	time.Sleep(20 * time.Millisecond)
	if got := c.Consume(); got != nil {
		t.Fatalf("expected nil after TTL, got %v", got)
	}
	c.Add([]int64{11})
	if got := c.Consume(); !reflect.DeepEqual(got, []int64{11}) {
		t.Fatalf("expected fresh entry, got %v", got)
	}
}

func TestAddEmptyNoop(t *testing.T) {
	c := New(time.Hour)
	c.Add(nil)
	if got := c.Consume(); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
