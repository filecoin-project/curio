package seal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type sdrPacingTime struct {
	elapsed time.Duration
	wall    time.Time
}

// Wall time chooses a stable phase once. Elapsed time controls all subsequent
// waiting and minimum intervals, independently of wall-clock adjustments.
type sdrStartPacer struct {
	mu         sync.Mutex
	interval   time.Duration
	jitter     bool
	offset     time.Duration
	now        func() sdrPacingTime
	lastStart  time.Duration
	started    bool
	phaseStart time.Duration
	phaseWait  time.Duration
	phaseSet   bool
	sequence   uint64
	reserved   uint64
	observer   sdrPacingObserver
}

func newSDRStartPacer(interval time.Duration, jitter bool, identity string, now func() sdrPacingTime) (*sdrStartPacer, error) {
	if interval < 0 {
		return nil, fmt.Errorf("SealSDRMinStartInterval must not be negative: %s", interval)
	}
	if interval == 0 {
		return nil, nil
	}
	if jitter && strings.TrimSpace(identity) == "" {
		return nil, fmt.Errorf("SDR start jitter requires a stable instance identity")
	}
	if now == nil {
		origin := time.Now()
		now = func() sdrPacingTime {
			n := time.Now()
			return sdrPacingTime{elapsed: n.Sub(origin), wall: n}
		}
	}
	p := &sdrStartPacer{interval: interval, jitter: jitter, now: now}
	if jitter {
		sum := sha256.Sum256([]byte(identity))
		p.offset = time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(interval))
	}
	return p, nil
}

func (p *sdrStartPacer) reserve() (uint64, bool) {
	token, ok, snapshot := p.reserveSnapshot()
	if !ok {
		p.observer.blocked(snapshot)
	}
	return token, ok
}

func (p *sdrStartPacer) reserveSnapshot() (uint64, bool, sdrPacingSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reserved != 0 {
		return 0, false, p.snapshotLocked(p.now())
	}
	n := p.now()
	if p.started && n.elapsed-p.lastStart < p.interval {
		return 0, false, p.snapshotLocked(n)
	}
	// Subtraction avoids overflow for large configured durations.
	idle := !p.started || (n.elapsed-p.lastStart > p.interval && n.elapsed-p.lastStart-p.interval > p.interval)
	if p.jitter && !p.phaseSet && idle {
		p.phaseStart = n.elapsed
		p.phaseWait = nextSDRPhaseDelay(n.wall, p.interval, p.offset)
		p.phaseSet = true
	}
	if p.phaseSet && n.elapsed-p.phaseStart < p.phaseWait {
		return 0, false, p.snapshotLocked(n)
	}
	p.sequence++
	if p.sequence == 0 {
		p.sequence++
	}
	p.reserved = p.sequence
	return p.reserved, true, sdrPacingSnapshot{}
}

func (p *sdrStartPacer) start(ctx context.Context, token uint64, taskID harmonytask.TaskID) error {
	snapshot, err := p.startSnapshot(ctx, token)
	if err == nil {
		p.observer.emit(sdrPacingEvent{kind: sdrPacingStarted, snapshot: snapshot, token: token, taskID: taskID})
	}
	return err
}

func (p *sdrStartPacer) startAdmission(ctx context.Context, token uint64, taskID harmonytask.TaskID) error {
	snapshot, err := p.startSnapshot(ctx, token)
	if err == nil {
		p.observer.entry(sdrPacingEvent{kind: sdrPacingStarted, snapshot: snapshot, token: token, taskID: taskID})
	}
	return err
}

func (p *sdrStartPacer) startSnapshot(ctx context.Context, token uint64) (sdrPacingSnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token == 0 || p.reserved != token {
		return sdrPacingSnapshot{}, fmt.Errorf("SDR start reservation no longer belongs to this attempt")
	}
	if err := ctx.Err(); err != nil {
		p.reserved = 0
		return sdrPacingSnapshot{}, err
	}
	n := p.now()
	p.lastStart = n.elapsed
	p.started = true
	p.phaseSet = false
	p.reserved = 0
	return p.snapshotLocked(n), nil
}

func (p *sdrStartPacer) cancel(token uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token != 0 && p.reserved == token {
		p.reserved = 0
	}
}

func nextSDRPhaseDelay(now time.Time, interval, offset time.Duration) time.Duration {
	// Compute modulo in two steps to avoid overflowing UnixNano - offset.
	rem := now.UnixNano() % int64(interval)
	if rem < 0 {
		rem += int64(interval)
	}
	if rem <= int64(offset) {
		return offset - time.Duration(rem)
	}
	return interval - (time.Duration(rem) - offset)
}

// ConfigureStartPacing is called once before registering the task. The listen
// identity separates instances sharing a host/name; it must remain stable
// across restarts. No live config mutation API is introduced.
func (s *SDRTask) ConfigureStartPacing(interval time.Duration, jitter bool, nodeName, listenIdentity string) error {
	identity := strings.TrimSpace(nodeName) + "\x00" + strings.TrimSpace(listenIdentity)
	if jitter && interval > 0 && (strings.TrimSpace(nodeName) == "" || strings.TrimSpace(listenIdentity) == "") {
		return fmt.Errorf("SDR start jitter requires CURIO_NODE_NAME and a stable instance listen identity")
	}
	p, err := newSDRStartPacer(interval, jitter, identity, nil)
	if err != nil {
		return err
	}
	s.startPacer = p
	p.logConfiguration(interval, jitter)
	return nil
}

func (s *SDRTask) ReserveTaskStart(taskID harmonytask.TaskID) (func(context.Context) error, func(), bool) {
	if s.startPacer == nil {
		return nil, nil, true
	}
	token, ok := s.startPacer.reserve()
	if !ok {
		return nil, nil, false
	}
	return func(ctx context.Context) error { return s.startPacer.startAdmission(ctx, token, taskID) }, func() { s.startPacer.cancel(token) }, true
}

// TaskStartBlocked avoids synchronous readiness queries during a known pacing
// wait. It does not evaluate a new idle phase or consume speculative admission.
// A due admission still revalidates candidates before attempting reservation.
func (s *SDRTask) TaskStartBlocked() bool {
	if s.startPacer == nil {
		return false
	}
	snapshot := s.startPacer.snapshot()
	blocked := snapshot.reserved != 0 || snapshot.remaining > 0
	if blocked {
		s.startPacer.observer.blocked(snapshot)
	}
	return blocked
}
