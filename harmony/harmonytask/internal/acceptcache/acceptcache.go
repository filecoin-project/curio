// Package acceptcache stores the result of a CanAccept() call so the
// scheduler can reuse it on the next considerWork cycle without paying the
// (potentially expensive) cost of re-evaluating CanAccept. The entries
// expire after a configurable TTL to avoid acting on stale decisions.
//
// The slice, timestamp, and mutex are unexported, so callers can only
// interact through Add / Consume, which always acquire the lock correctly.
package acceptcache

import (
	"sync"
	"time"
)

// Cache is a TTL-bounded bucket of accepted task IDs. Multiple producers
// (the background poller, the scheduler writing leftover remainders) may
// call Add concurrently with the scheduler calling Consume.
//
// Add de-duplicates: the same task ID added more than once between Consume
// calls is stored once. Without this, a backlog that stays unowned across
// many poll cycles would re-Add the full set every cycle and grow the slice
// unboundedly (the TTL never expires while Add keeps refreshing it).
type Cache struct {
	ttl time.Duration

	mu           sync.Mutex
	ids          []int64
	seen         map[int64]struct{}
	lastAccepted time.Time
}

// New constructs a Cache whose entries expire after ttl.
func New(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, seen: make(map[int64]struct{})}
}

// Add appends not-yet-cached ids to the cache and refreshes the TTL baseline.
// Duplicate ids (already present since the last Consume) are skipped. Callers
// may add from any goroutine.
func (c *Cache) Add(ids []int64) {
	if len(ids) == 0 {
		return
	}
	c.mu.Lock()
	for _, id := range ids {
		if _, ok := c.seen[id]; ok {
			continue
		}
		c.seen[id] = struct{}{}
		c.ids = append(c.ids, id)
	}
	c.lastAccepted = time.Now()
	c.mu.Unlock()
}

// Consume returns all cached ids and clears the cache. If the TTL has
// elapsed since the last Add, the cache is discarded and nil is returned
// (the caller should fall back to calling CanAccept).
func (c *Cache) Consume() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.lastAccepted) > c.ttl {
		c.ids = nil
		c.seen = make(map[int64]struct{})
		return nil
	}
	out := c.ids
	c.ids = nil
	c.seen = make(map[int64]struct{})
	return out
}
