package taskhelp

import (
	"context"
	"errors"
	"sync"
	"time"
)

// WorkerBackoff bounds attempts on a known unavailable worker. It has no DB,
// native probe or timer goroutine. Callers classify failures before Result.
// After 2 minutes, at most one ordinary attempt probes recovery; consecutive
// failures increase the interval up to 30 minutes. Zero value is healthy.
type WorkerBackoff struct {
	mu                          sync.Mutex
	now                         func() time.Time
	until                       time.Time
	delay                       time.Duration
	probe, sequence, generation uint64
}

func NewWorkerBackoff(now func() time.Time) *WorkerBackoff { return &WorkerBackoff{now: now} }
func (g *WorkerBackoff) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}
func (g *WorkerBackoff) Blocked() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.probe != 0 || g.clock().Before(g.until)
}
func (g *WorkerBackoff) Epoch() uint64 { g.mu.Lock(); defer g.mu.Unlock(); return g.generation }
func (g *WorkerBackoff) Reserve() (func(context.Context) error, func(), bool) {
	g.mu.Lock()
	if g.probe != 0 || g.clock().Before(g.until) {
		g.mu.Unlock()
		return nil, nil, false
	}
	if g.delay == 0 {
		g.mu.Unlock()
		return nil, nil, true
	}
	g.sequence++
	token := g.sequence
	g.probe = token
	g.mu.Unlock()
	return func(ctx context.Context) error { return ctx.Err() }, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.probe == token {
			g.probe = 0
		}
	}, true
}

// Result returns the remaining hold for diagnostics; logging must occur outside
// the mutex. A success from before a newer failure cannot reopen the circuit.
func (g *WorkerBackoff) Result(epoch uint64, err error) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	var unavailable *WorkerUnavailable
	if errors.As(err, &unavailable) {
		g.generation++
		g.delay = min(max(2*time.Minute, g.delay*2), 30*time.Minute)
		g.until = g.clock().Add(g.delay)
	} else if epoch == g.generation {
		if err == nil {
			g.delay = 0
			g.until = time.Time{}
		} else if g.delay > 0 {
			g.until = g.clock().Add(g.delay)
		}
	}
	return max(0, g.until.Sub(g.clock()))
}
