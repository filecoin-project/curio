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

func TestTakeMatchingPartialLeavesUnmatched(t *testing.T) {
	c := New(time.Hour)
	c.Add([]int64{1, 2, 3})
	matched, hadFresh := c.TakeMatching([]int64{2, 9})
	if !hadFresh {
		t.Fatal("expected hadFresh")
	}
	if !reflect.DeepEqual(matched, []int64{2}) {
		t.Fatalf("matched=%v", matched)
	}
	left := c.Consume()
	if !reflect.DeepEqual(left, []int64{1, 3}) {
		t.Fatalf("left in cache=%v", left)
	}
}

func TestTakeMatchingNoOverlapIsMissNotEmptyCacheDenial(t *testing.T) {
	c := New(time.Hour)
	c.Add([]int64{1, 2})
	matched, hadFresh := c.TakeMatching([]int64{9, 10})
	if !hadFresh {
		t.Fatal("expected hadFresh when cache had entries")
	}
	if len(matched) != 0 {
		t.Fatalf("expected no match, got %v", matched)
	}
	left := c.Consume()
	if !reflect.DeepEqual(left, []int64{1, 2}) {
		t.Fatalf("unmatched should remain cached, got %v", left)
	}
}

func TestTakeMatchingEmptyOrExpired(t *testing.T) {
	c := New(time.Hour)
	matched, hadFresh := c.TakeMatching([]int64{1})
	if hadFresh || matched != nil {
		t.Fatalf("empty cache: matched=%v hadFresh=%v", matched, hadFresh)
	}

	c = New(5 * time.Millisecond)
	c.Add([]int64{1})
	time.Sleep(20 * time.Millisecond)
	matched, hadFresh = c.TakeMatching([]int64{1})
	if hadFresh || matched != nil {
		t.Fatalf("expired: matched=%v hadFresh=%v", matched, hadFresh)
	}
}
