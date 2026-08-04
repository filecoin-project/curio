// Package runregistry tracks the set of currently-executing tasks on a
// single node. The registry's internal map and mutex are unexported, so
// callers cannot read or mutate state without going through methods that
// acquire the lock correctly.
//
// A Handle is issued when a task starts. The scheduler and preempt paths
// interact with running tasks exclusively through Handle methods (Preempt,
// IsPreempted, WaitDone, StartTime) and through Registry.Snapshot, so there
// is no way to forget to lock.
package runregistry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Registry is a concurrent map of running task handles keyed by task ID.
// Zero value is not usable; call New.
type Registry struct {
	mu    sync.Mutex
	tasks map[int64]*Handle
}

// Handle is the caller-visible control surface for a running task. All fields
// are either immutable after Start (startTime, cancel, done) or atomic
// (preempted), so reading them does not require the Registry mutex.
type Handle struct {
	id        int64
	startTime time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	preempted atomic.Bool
}

// Entry is an immutable snapshot of a running task's observable state,
// suitable for callers that need to iterate without holding the Registry lock.
type Entry struct {
	ID        int64
	StartTime time.Time
	Preempted bool
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{tasks: make(map[int64]*Handle)}
}

// Start records a newly-running task and returns its Handle. The cancel
// function is stored so Preempt can cancel the task's context. The caller
// is responsible for calling Finish when the task's goroutine exits so the
// Handle's done channel is closed and the registry entry is removed.
func (r *Registry) Start(id int64, cancel context.CancelFunc) *Handle {
	h := &Handle{
		id:        id,
		startTime: time.Now(),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	r.mu.Lock()
	r.tasks[id] = h
	r.mu.Unlock()
	return h
}

// Finish removes the task from the registry and closes its done channel so
// preempt waiters wake up. It is safe to call at most once per Start.
func (r *Registry) Finish(id int64) {
	r.mu.Lock()
	h, ok := r.tasks[id]
	if ok {
		delete(r.tasks, id)
	}
	r.mu.Unlock()
	if ok {
		close(h.done)
	}
}

// Get returns the Handle for the given task, or (nil, false) if the task is
// not currently running.
func (r *Registry) Get(id int64) (*Handle, bool) {
	r.mu.Lock()
	h, ok := r.tasks[id]
	r.mu.Unlock()
	return h, ok
}

// Snapshot returns an immutable view of every running task. The slice is
// unordered.
func (r *Registry) Snapshot() []Entry {
	r.mu.Lock()
	out := make([]Entry, 0, len(r.tasks))
	for id, h := range r.tasks {
		out = append(out, Entry{
			ID:        id,
			StartTime: h.startTime,
			Preempted: h.preempted.Load(),
		})
	}
	r.mu.Unlock()
	return out
}

// StartTime returns the wall-clock moment the task began.
func (h *Handle) StartTime() time.Time { return h.startTime }

// IsPreempted reports whether Preempt has been called on this handle.
func (h *Handle) IsPreempted() bool { return h.preempted.Load() }

// Preempt marks the handle as preempted and cancels its context. Callers may
// then WaitDone to bound how long they wait for the goroutine to exit.
func (h *Handle) Preempt() {
	h.preempted.Store(true)
	if h.cancel != nil {
		h.cancel()
	}
}

// WaitDone blocks until the task's goroutine exits (Registry.Finish is called)
// or until the supplied deadline channel fires. It reports whether the task
// exited before the deadline.
func (h *Handle) WaitDone(deadline <-chan time.Time) (exited bool) {
	select {
	case <-h.done:
		return true
	case <-deadline:
		return false
	}
}
