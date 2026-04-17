// Package runnowflags is a small, concurrent registry of per-task-name
// "run now" flags. A background poller sets a flag when it sees a
// run_now_request row in the DB; the SingletonTaskAdder for that task
// reads and clears it to bypass the normal interval on the next tick.
//
// The map and mutex are unexported; callers reach flags through Flag() and
// iterate through Snapshot(), never touching the protected fields directly.
package runnowflags

import (
	"sync"
	"sync/atomic"
)

// Registry maps task name -> *atomic.Bool flag.
type Registry struct {
	mu    sync.Mutex
	flags map[string]*atomic.Bool
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{flags: make(map[string]*atomic.Bool)}
}

// Flag returns the atomic flag for name, creating it on first use. The
// returned pointer is stable for the life of the Registry.
func (r *Registry) Flag(name string) *atomic.Bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f, ok := r.flags[name]; ok {
		return f
	}
	f := new(atomic.Bool)
	r.flags[name] = f
	return f
}

// Snapshot returns a shallow copy of the flag map. The copy is safe to
// iterate without holding the registry lock; flag pointers are shared so
// Store/Load on them affects the real state.
func (r *Registry) Snapshot() map[string]*atomic.Bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]*atomic.Bool, len(r.flags))
	for k, v := range r.flags {
		out[k] = v
	}
	return out
}
