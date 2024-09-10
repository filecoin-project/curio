package taskhelp

import (
	"sync/atomic"
)

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

func Max(n int) *MaxCounter {
	return &MaxCounter{N: n, current: new(atomic.Int32), currentThis: new(atomic.Int32)}
}
