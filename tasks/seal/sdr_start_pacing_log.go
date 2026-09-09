package seal

import (
	"sync"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

const sdrBlockedLogInterval = time.Minute

const (
	sdrPacingConfigured = "SDR start pacing configured"
	sdrPacingBlocked    = "SDR start delayed"
	sdrPacingStarted    = "SDR Do entry committed"
)

// A value-only snapshot: formatting and logging never read the live pacer.
// next_start_at is a wall-clock estimate from monotonic remaining time, not a
// reservation or a cluster-wide start promise.
type sdrPacingSnapshot struct {
	observed  sdrPacingTime
	interval  time.Duration
	jitter    bool
	offset    time.Duration
	reserved  uint64
	reason    string
	remaining time.Duration
	nextKnown bool
}

// The caller holds p.mu. Observation must not latch a phase, reserve a token,
// consume an interval, or change the clock origin.
func (p *sdrStartPacer) snapshotLocked(n sdrPacingTime) sdrPacingSnapshot {
	s := sdrPacingSnapshot{observed: n, interval: p.interval, jitter: p.jitter, offset: p.offset, reserved: p.reserved}
	switch {
	case p.reserved != 0:
		s.reason = "reservation pending"
	case p.started && n.elapsed-p.lastStart < p.interval:
		s.reason = "min start interval"
		s.remaining = p.interval - (n.elapsed - p.lastStart)
		s.nextKnown = true
	case p.phaseSet && n.elapsed-p.phaseStart < p.phaseWait:
		s.reason = "start jitter phase"
		s.remaining = p.phaseWait - (n.elapsed - p.phaseStart)
		s.nextKnown = true
	default:
		// Read-only diagnostics do not evaluate/latch a new idle phase.
		s.reason = "no active wait; eligibility not evaluated"
	}
	return s
}

func (p *sdrStartPacer) snapshot() sdrPacingSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotLocked(p.now())
}

type sdrPacingEvent struct {
	kind     string
	snapshot sdrPacingSnapshot
	token    uint64
	taskID   harmonytask.TaskID
}

// Diagnostic throttling is separate from admission state. The sink is fixed
// before use; nil selects the ordinary seal logger. Neither mutex is held
// across the sink, including when an observer re-enters diagnostics.
type sdrPacingObserver struct {
	mu             sync.Mutex
	blockedLogged  bool
	lastBlockedLog time.Duration
	sink           func(sdrPacingEvent)
}

func (o *sdrPacingObserver) blocked(s sdrPacingSnapshot) {
	o.mu.Lock()
	shouldLog := !o.blockedLogged || s.observed.elapsed-o.lastBlockedLog >= sdrBlockedLogInterval
	if shouldLog {
		o.blockedLogged = true
		o.lastBlockedLog = s.observed.elapsed
	}
	o.mu.Unlock()
	if shouldLog {
		o.emit(sdrPacingEvent{kind: sdrPacingBlocked, snapshot: s})
	}
}

func (o *sdrPacingObserver) emit(e sdrPacingEvent) {
	if o != nil && o.sink != nil {
		o.sink(e)
		return
	}
	log.Infow(e.kind, e.fields()...)
}

func (p *sdrStartPacer) logConfiguration(interval time.Duration, jitter bool) {
	if p == nil {
		// Preserve zero-interval behavior even when the jitter flag is set.
		(*sdrPacingObserver)(nil).emit(sdrPacingEvent{kind: sdrPacingConfigured, snapshot: sdrPacingSnapshot{interval: interval, jitter: jitter}})
		return
	}
	p.observer.emit(sdrPacingEvent{kind: sdrPacingConfigured, snapshot: p.snapshot()})
}

func (e sdrPacingEvent) fields() []interface{} {
	s := e.snapshot
	fields := []interface{}{
		"pacing_unit", "process-local SDR Do entry",
		"pacing_enabled", s.interval > 0,
		"min_start_interval", s.interval.String(),
		"start_jitter", s.jitter,
		"jitter_offset", s.offset.String(),
	}
	if e.kind == sdrPacingConfigured {
		return append(fields, "blocked_log_interval", sdrBlockedLogInterval.String())
	}
	remaining, next := "unknown", "unknown"
	if s.nextKnown {
		remaining = s.remaining.String()
		next = s.observed.wall.Add(s.remaining).Format(time.RFC3339Nano)
	}
	fields = append(fields,
		"observed_at", s.observed.wall.Format(time.RFC3339Nano),
		"reason", s.reason,
		"remaining", remaining,
		"next_start_at", next,
		"next_start_known", s.nextKnown,
	)
	if e.kind == sdrPacingStarted {
		return append(fields, "task", e.taskID, "reservation_token", e.token)
	}
	return append(fields, "reservation_token", s.reserved)
}
