package acceptcache

import (
	"reflect"
	"sync"
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

func TestStoreEmptyRemembersCheckedCandidates(t *testing.T) {
	c := New(time.Hour)
	c.Store([]int64{1, 2}, nil)
	matched, checked := c.GetMatching([]int64{1, 2})
	if len(matched) != 0 || !checked {
		t.Fatalf("empty decision: matched=%v checked=%v", matched, checked)
	}
	matched, checked = c.GetMatching([]int64{1, 3})
	if len(matched) != 0 || checked {
		t.Fatalf("unknown candidate: matched=%v checked=%v", matched, checked)
	}
	if matched, hadFresh := c.TakeMatching([]int64{1}); len(matched) != 0 || hadFresh {
		t.Fatalf("empty accepted set: matched=%v hadFresh=%v", matched, hadFresh)
	}
	if _, checked := c.GetMatching([]int64{1, 2}); !checked {
		t.Fatal("TakeMatching discarded checked-empty evaluation")
	}
}

func TestGetMatchingReturnsPartialCoverageWithoutConsuming(t *testing.T) {
	c := New(time.Hour)
	c.Store([]int64{1, 2, 3}, []int64{3, 1})
	for i := 0; i < 2; i++ {
		matched, checked := c.GetMatching([]int64{1, 3, 4})
		if !reflect.DeepEqual(matched, []int64{3, 1}) || checked {
			t.Fatalf("partial coverage: matched=%v checked=%v", matched, checked)
		}
	}
	matched, checked := c.GetMatching([]int64{1, 2, 3})
	if !reflect.DeepEqual(matched, []int64{3, 1}) || !checked {
		t.Fatalf("full coverage: matched=%v checked=%v", matched, checked)
	}
}

func TestStoreReplacesEvaluation(t *testing.T) {
	c := New(time.Hour)
	c.Store([]int64{1, 2}, []int64{1})
	c.Store([]int64{3, 4}, []int64{4, 4})
	matched, checked := c.GetMatching([]int64{1, 2})
	if len(matched) != 0 || checked {
		t.Fatalf("previous evaluation survived: matched=%v checked=%v", matched, checked)
	}
	matched, checked = c.GetMatching([]int64{3, 4})
	if !reflect.DeepEqual(matched, []int64{4}) || !checked {
		t.Fatalf("replacement evaluation: matched=%v checked=%v", matched, checked)
	}
}

func TestStoreEvaluationExpires(t *testing.T) {
	for _, accepted := range [][]int64{nil, {1}} {
		c := New(time.Minute)
		c.Store([]int64{1}, accepted)
		c.lastAccepted = time.Now().Add(-2 * time.Minute)
		c.Refresh()
		matched, checked := c.GetMatching([]int64{1})
		if len(matched) != 0 || checked {
			t.Fatalf("expired evaluation %v: matched=%v checked=%v", accepted, matched, checked)
		}
		c.Store([]int64{1}, accepted)
		if _, checked := c.GetMatching([]int64{1}); !checked {
			t.Fatal("Store did not start a fresh TTL")
		}
	}
}

func TestForgetAllowsRetryWithoutRefreshingTTL(t *testing.T) {
	c := New(time.Hour)
	c.Store([]int64{1, 2, 3}, []int64{1, 3})
	baseline := c.lastAccepted
	c.Forget([]int64{1, 2})
	for _, id := range []int64{1, 2} {
		matched, checked := c.GetMatching([]int64{id})
		if len(matched) != 0 || checked {
			t.Fatalf("forgotten candidate %d: matched=%v checked=%v", id, matched, checked)
		}
	}
	matched, checked := c.GetMatching([]int64{3})
	if !reflect.DeepEqual(matched, []int64{3}) || !checked {
		t.Fatalf("retained candidate: matched=%v checked=%v", matched, checked)
	}
	if c.lastAccepted != baseline {
		t.Fatal("Forget refreshed the TTL")
	}
}

func TestRefreshRetainsAcceptedAndClearsOtherCoverage(t *testing.T) {
	c := New(time.Hour)
	c.Store([]int64{1, 2, 3}, []int64{1, 2})
	c.TakeMatching([]int64{1})
	baseline := c.lastAccepted
	c.Refresh()
	for _, id := range []int64{1, 3} {
		matched, checked := c.GetMatching([]int64{id})
		if len(matched) != 0 || checked {
			t.Fatalf("candidate %d should be retried: matched=%v checked=%v", id, matched, checked)
		}
	}
	matched, checked := c.GetMatching([]int64{2})
	if !reflect.DeepEqual(matched, []int64{2}) || !checked {
		t.Fatalf("retained acceptance: matched=%v checked=%v", matched, checked)
	}
	if c.lastAccepted != baseline {
		t.Fatal("Refresh refreshed the TTL")
	}
}

func TestConsumingRetainsCheckedCoverage(t *testing.T) {
	for _, consumeAll := range []bool{false, true} {
		c := New(time.Hour)
		c.Add([]int64{1})
		if consumeAll {
			c.Consume()
		} else {
			c.TakeMatching([]int64{1})
		}
		matched, checked := c.GetMatching([]int64{1})
		if len(matched) != 0 || !checked {
			t.Fatalf("consumeAll=%v: matched=%v checked=%v", consumeAll, matched, checked)
		}
	}
}

func TestConcurrentCacheOperations(t *testing.T) {
	c := New(time.Hour)
	var wg sync.WaitGroup
	operations := []func(){
		func() { c.Store([]int64{1, 2, 3}, []int64{1, 2}) },
		func() { c.Add([]int64{2, 3}) },
		func() { c.GetMatching([]int64{1, 2, 3}) },
		func() { c.Forget([]int64{1}) },
		func() { c.Refresh() },
		func() { c.TakeMatching([]int64{2}) },
		func() { c.Consume() },
	}
	for _, operation := range operations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				operation()
			}
		}()
	}
	wg.Wait()
	c.Store([]int64{1, 2}, []int64{2})
	matched, checked := c.GetMatching([]int64{1, 2})
	if !reflect.DeepEqual(matched, []int64{2}) || !checked {
		t.Fatalf("final evaluation: matched=%v checked=%v", matched, checked)
	}
}
