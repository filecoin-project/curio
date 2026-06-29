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
type Cache struct {
	ttl time.Duration

	mu           sync.Mutex
	ids          []int64
	lastAccepted time.Time
}

// New constructs a Cache whose entries expire after ttl.
func New(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl}
}

// Add appends ids to the cache and refreshes the TTL baseline. Callers may
// add from any goroutine.
func (c *Cache) Add(ids []int64) {
	if len(ids) == 0 {
		return
	}
	c.mu.Lock()
	c.ids = append(c.ids, ids...)
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
		return nil
	}
	out := c.ids
	c.ids = nil
	return out
}
