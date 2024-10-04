package taskhelp

import (
	"sync/atomic"
)

type Limiter interface {
	// Active returns the number of tasks of this type that are currently running
	// in this limiter / limiter group.
	Active() int

	// ActiveThis returns the number of tasks of this type that are currently running
	// in this limiter (e.g. per-task-type count).
	ActiveThis() int

	// AtMax returns whether this limiter permits more tasks to run.
	AtMax() bool

	// Add increments / decrements the active task counters by delta. This call
	// is atomic
	Add(delta int)

	// Instance spawns a sub-instance of this limiter. This is called by harmonytask on startup for each task
	// using this limiter. Each sub-instance has it's own individual "This" counter, but can share a common counter.
	Instance() Limiter
}

type MaxCounter struct {
	// maximum number of tasks of this type that can be run
	N int

	// current number of tasks of this type that are running (shared)
	current *atomic.Int32

	// current number of tasks of this type that are running (per task)
	currentThis *atomic.Int32
}

func (m *MaxCounter) AtMax() bool {
	return m.Max() > 0 && m.Active() >= m.Max()
}

func (m *MaxCounter) Max() int {
	return m.N
}

// note: cur can't be called on counters for which max is 0
func (m *MaxCounter) Active() int {
	return int(m.current.Load())
}

func (m *MaxCounter) ActiveThis() int {
	return int(m.currentThis.Load())
}

func (m *MaxCounter) Add(n int) {
	m.current.Add(int32(n))
	m.currentThis.Add(int32(n))
}

func (m *MaxCounter) Instance() Limiter {
	return &MaxCounter{N: m.N, current: m.current, currentThis: new(atomic.Int32)}
}

func Max(n int) *MaxCounter {
	return &MaxCounter{N: n, current: new(atomic.Int32), currentThis: new(atomic.Int32)}
}
