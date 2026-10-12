// Package acceptcache stores the result of a CanAccept() call so the
// scheduler can reuse it on the next considerWork cycle without paying the
// (potentially expensive) cost of re-evaluating CanAccept. The entries
// expire after a configurable TTL to avoid acting on stale decisions.
package acceptcache

import (
	"sync"
	"time"
)

// Cache stores accepted task IDs and the candidates checked by CanAccept.
// All methods are safe to call concurrently.
type Cache struct {
	ttl time.Duration

	mu           sync.Mutex
	ids          []int64
	seen         map[int64]struct{}
	checked      map[int64]struct{}
	lastAccepted time.Time
}

// New constructs a Cache whose entries expire after ttl.
func New(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, seen: make(map[int64]struct{}), checked: make(map[int64]struct{})}
}

// Add appends accepted IDs, marks them checked, and refreshes the TTL baseline.
// Duplicate IDs are skipped.
func (c *Cache) Add(ids []int64) {
	if len(ids) == 0 {
		return
	}
	c.mu.Lock()
	for _, id := range ids {
		c.checked[id] = struct{}{}
		if _, ok := c.seen[id]; ok {
			continue
		}
		c.seen[id] = struct{}{}
		c.ids = append(c.ids, id)
	}
	c.lastAccepted = time.Now()
	c.mu.Unlock()
}

// Store replaces the cached evaluation, including an empty accepted result.
func (c *Cache) Store(candidates, accepted []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ids = nil
	c.seen = make(map[int64]struct{}, len(accepted))
	c.checked = make(map[int64]struct{}, len(candidates))
	for _, id := range candidates {
		c.checked[id] = struct{}{}
	}
	for _, id := range accepted {
		if _, ok := c.seen[id]; ok {
			continue
		}
		c.seen[id] = struct{}{}
		c.ids = append(c.ids, id)
	}
	c.lastAccepted = time.Now()
}

// GetMatching returns accepted candidates without consuming them. checked is
// true only when every candidate has a fresh evaluation; matched can still
// contain accepted IDs when some candidates have not been checked.
func (c *Cache) GetMatching(candidates []int64) (matched []int64, checked bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.lastAccepted) > c.ttl {
		c.ids = nil
		c.seen = make(map[int64]struct{})
		c.checked = make(map[int64]struct{})
		return nil, false
	}

	checked = true
	cand := make(map[int64]struct{}, len(candidates))
	for _, id := range candidates {
		cand[id] = struct{}{}
		if _, ok := c.checked[id]; !ok {
			checked = false
		}
	}
	for _, id := range c.ids {
		if _, ok := cand[id]; ok {
			matched = append(matched, id)
		}
	}
	return matched, checked
}

// Forget removes acceptance and checked coverage without refreshing the TTL.
func (c *Cache) Forget(ids []int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		delete(c.seen, id)
		delete(c.checked, id)
	}
	keep := c.ids[:0]
	for _, id := range c.ids {
		if _, ok := c.seen[id]; ok {
			keep = append(keep, id)
		}
	}
	c.ids = keep
}

// Refresh allows refused or consumed IDs to be checked again while retaining
// accepted IDs. It does not refresh the TTL.
func (c *Cache) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = make(map[int64]struct{}, len(c.ids))
	for _, id := range c.ids {
		c.checked[id] = struct{}{}
	}
}

// Consume returns all cached accepted IDs, retaining their checked coverage.
// Expired evaluations are discarded and return nil.
func (c *Cache) Consume() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.lastAccepted) > c.ttl {
		c.ids = nil
		c.seen = make(map[int64]struct{})
		c.checked = make(map[int64]struct{})
		return nil
	}
	out := c.ids
	c.ids = nil
	c.seen = make(map[int64]struct{})
	return out
}

// TakeMatching removes and returns cached ids that appear in candidates.
// Unmatched cached ids are left in place for later considerWork calls.
//
// hadFresh is false when the cache is empty or TTL-expired (caller should
// call CanAccept for the full candidate set). hadFresh is true when the
// cache held live entries — even if matched is empty. An empty match with
// hadFresh true is a cache miss for this candidate set, not a CanAccept
// refusal; the caller must call CanAccept rather than treating it as deny.
func (c *Cache) TakeMatching(candidates []int64) (matched []int64, hadFresh bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.lastAccepted) > c.ttl {
		c.ids = nil
		c.seen = make(map[int64]struct{})
		c.checked = make(map[int64]struct{})
		return nil, false
	}
	if len(c.ids) == 0 {
		return nil, false
	}

	cand := make(map[int64]struct{}, len(candidates))
	for _, id := range candidates {
		cand[id] = struct{}{}
	}

	keep := make([]int64, 0, len(c.ids))
	keepSeen := make(map[int64]struct{}, len(c.ids))
	for _, id := range c.ids {
		if _, ok := cand[id]; ok {
			matched = append(matched, id)
			continue
		}
		keep = append(keep, id)
		keepSeen[id] = struct{}{}
	}
	c.ids = keep
	c.seen = keepSeen
	return matched, true
}
