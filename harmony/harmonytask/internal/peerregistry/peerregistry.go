// Package peerregistry manages the in-memory routing table from task-type
// names to peer connections. The peers map, per-task-type index, and the
// mutex protecting them are unexported, so the harmonytask package can only
// interact through methods that lock correctly.
//
// Typical lifecycle:
//
//	remove := registry.Add(peerID, addr, conn, []string{"Seal","SDR"})
//	defer remove()
//	// ... receive loop ...
//
// Broadcast fans a message out to every peer that handles a given task
// type, each in its own goroutine, so the scheduler thread never blocks on
// network I/O.
//
// # Storage model
//
// Peers are stored in a map keyed by peer ID, so disconnect is a simple
// delete — no zombie entries, no stable-index invariant to uphold.
//
// A per-entry generation counter disambiguates reconnects: if the same
// peer ID is re-Added before the previous connection's remove() fires,
// the stale remove() closure observes a generation mismatch and leaves
// the newer entry alone. This mirrors the original slice-index behaviour
// (each Add gets its own distinct slot) without the append-only baggage.
package peerregistry

import (
	"sync"
)

// Conn is the minimum contract needed for fan-out delivery. Callers
// typically satisfy this with a harmonytask.PeerConnection adapter.
type Conn interface {
	SendMessage([]byte) error
}

// Registry is the concurrent peer routing table.
type Registry struct {
	mu      sync.RWMutex
	nextGen uint64
	peers   map[int64]peer
	// byTask maps task-type -> set of peer IDs registered for that type.
	// Membership here is the single source of truth for "is this peer
	// routable for this task?". Entries are deleted promptly on remove.
	byTask map[string]map[int64]struct{}
}

type peer struct {
	id    int64
	gen   uint64
	addr  string
	conn  Conn
	tasks map[string]struct{}
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		peers:  make(map[int64]peer),
		byTask: make(map[string]map[int64]struct{}),
	}
}

// Add registers a peer and the set of task types it can handle. The
// returned remove function is safe to call at most once per Add (guarded
// internally by sync.Once), and safe to `defer`.
//
// If another Add with the same id is made before this Add's remove fires,
// the newer entry replaces the older one. The older remove() closure
// becomes a no-op (generation mismatch) so it cannot accidentally evict
// the replacement.
func (r *Registry) Add(id int64, addr string, conn Conn, tasks []string) (remove func()) {
	taskSet := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		taskSet[t] = struct{}{}
	}

	r.mu.Lock()
	if existing, ok := r.peers[id]; ok {
		for t := range existing.tasks {
			if set := r.byTask[t]; set != nil {
				delete(set, id)
				if len(set) == 0 {
					delete(r.byTask, t)
				}
			}
		}
	}
	r.nextGen++
	gen := r.nextGen
	r.peers[id] = peer{
		id:    id,
		gen:   gen,
		addr:  addr,
		conn:  conn,
		tasks: taskSet,
	}
	for t := range taskSet {
		set := r.byTask[t]
		if set == nil {
			set = make(map[int64]struct{})
			r.byTask[t] = set
		}
		set[id] = struct{}{}
	}
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { r.removeGen(id, gen) })
	}
}

// removeGen takes the peer (id, gen) out of routing, but only if the
// currently-registered entry still matches the given generation. This
// prevents a stale remove() from a reconnected peer's previous session
// from evicting the replacement entry.
func (r *Registry) removeGen(id int64, gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers[id]
	if !ok || p.gen != gen {
		return
	}
	delete(r.peers, id)
	for t := range p.tasks {
		set := r.byTask[t]
		if set == nil {
			continue
		}
		delete(set, id)
		if len(set) == 0 {
			delete(r.byTask, t)
		}
	}
}

// HasAny reports whether any peer is registered to handle any task type.
func (r *Registry) HasAny() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byTask) > 0
}

// CountFor returns the number of peers currently handling taskType.
func (r *Registry) CountFor(taskType string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byTask[taskType])
}

// ErrorHook reports a send failure to a specific peer address. Consumers
// typically hook this to their logger.
type ErrorHook func(addr string, err error)

// Broadcast fans msg out to every peer registered for taskType. Each send
// runs in its own goroutine so slow peers do not block the caller. Send
// errors are reported to errFn if non-nil.
//
// The peer value (conn + addr) is copied into the goroutine while the
// read lock is held, so the goroutine cannot race with a concurrent Add
// or remove.
func (r *Registry) Broadcast(taskType string, msg []byte, errFn ErrorHook) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id := range r.byTask[taskType] {
		p, ok := r.peers[id]
		if !ok {
			continue
		}
		go func() {
			if err := p.conn.SendMessage(msg); err != nil && errFn != nil {
				errFn(p.addr, err)
			}
		}()
	}
}
