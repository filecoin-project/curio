// Package preemptbids brokers cross-node preempt-cost responses for a
// time-sensitive scheduling decision.
//
// Responsibilities:
//   - Register(id, buf): the node about to preempt registers a channel that
//     will receive peer cost responses. Any responses that arrived before
//     the channel existed are drained into the new channel.
//   - Deliver(id, resp): the peering receive path calls this when a cost
//     message comes in. If a channel is registered, delivery is non-blocking
//     and drops if full. Otherwise the response is buffered until Register.
//
// The internal maps and mutex are unexported; callers cannot reach them and
// therefore cannot forget to lock.
package preemptbids

import (
	"sync"
	"time"
)

// Response is the cost report from one peer for a specific task ID.
type Response struct {
	PeerID int64
	Cost   time.Duration
}

// Registry brokers pending and live response channels per task ID.
type Registry struct {
	mu       sync.Mutex
	channels map[int64]chan Response
	pending  map[int64][]Response
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		channels: make(map[int64]chan Response),
		pending:  make(map[int64][]Response),
	}
}

// Register returns a channel that will receive cost responses for id. Any
// responses delivered before registration are pre-loaded into the channel,
// so the caller never misses an early response. buffer sizes the channel;
// it must be large enough to hold pre-loaded plus expected live responses.
//
// The returned cancel func deregisters the channel, drops any queued
// responses, and may be called once; it is safe to defer.
func (r *Registry) Register(id int64, buffer int) (<-chan Response, func()) {
	r.mu.Lock()
	pending := r.pending[id]
	delete(r.pending, id)
	size := buffer
	if size < len(pending) {
		size = len(pending)
	}
	ch := make(chan Response, size)
	for _, p := range pending {
		ch <- p
	}
	r.channels[id] = ch
	r.mu.Unlock()

	cancel := func() {
		r.mu.Lock()
		delete(r.channels, id)
		delete(r.pending, id)
		r.mu.Unlock()
	}
	return ch, cancel
}

// Deliver routes a response to the registered channel for id, or buffers
// it under pending if no channel is registered yet. Delivery to a registered
// channel is non-blocking; if the channel is full the response is dropped.
func (r *Registry) Deliver(id int64, resp Response) {
	r.mu.Lock()
	if ch, ok := r.channels[id]; ok {
		select {
		case ch <- resp:
		default:
		}
		r.mu.Unlock()
		return
	}
	r.pending[id] = append(r.pending[id], resp)
	r.mu.Unlock()
}
