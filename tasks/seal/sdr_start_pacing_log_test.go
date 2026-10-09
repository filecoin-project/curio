package seal

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

// Deliberately excludes only the diagnostic rate limiter and immutable clock
// function. Every admission field is compared, including the reservation token.
func pacingState(p *sdrStartPacer) interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return struct {
		interval, offset, lastStart, phaseStart, phaseWait time.Duration
		jitter, started, phaseSet                          bool
		sequence, reserved                                 uint64
	}{p.interval, p.offset, p.lastStart, p.phaseStart, p.phaseWait, p.jitter, p.started, p.phaseSet, p.sequence, p.reserved}
}

func pacingFields(e sdrPacingEvent) map[string]interface{} {
	f := e.fields()
	m := make(map[string]interface{}, len(f)/2)
	for i := 0; i < len(f); i += 2 {
		m[f[i].(string)] = f[i+1]
	}
	return m
}

func TestSDRPacingDiagnosticsAreReadOnly(t *testing.T) {
	for _, stage := range []string{"unlatched", "phase", "reserved", "started"} {
		t.Run(stage, func(t *testing.T) {
			p, n := testSDRPacer(t, time.Minute, true)
			p.offset = 7 * time.Second
			var events []sdrPacingEvent
			p.observer.sink = func(e sdrPacingEvent) {
				events = append(events, e)
				_ = pacingFields(e)
				// Snapshot values cannot modify the pacer, even from a sink.
				e.snapshot.interval = 0
				e.snapshot.reserved = 0
			}
			if stage != "unlatched" {
				p.reserve()
			}
			if stage == "reserved" || stage == "started" {
				n.elapsed = p.phaseWait
				token, ok := p.reserve()
				if !ok {
					t.Fatal("phase fixture did not become eligible")
				}
				if stage == "started" {
					if err := p.start(context.Background(), token, 7); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, clock := pacingState(p), *n
			for range 100 {
				p.logConfiguration(p.interval, p.jitter)
				snapshot := p.snapshot()
				_ = pacingFields(sdrPacingEvent{kind: sdrPacingBlocked, snapshot: snapshot})
				if stage != "unlatched" {
					p.observer.blocked(snapshot)
				}
			}
			if !reflect.DeepEqual(before, pacingState(p)) || *n != clock {
				t.Fatal("diagnostic generation/logging changed admission or clock state")
			}
			if len(events) < 100 {
				t.Fatal("logging sink was not exercised")
			}
		})
	}
}

func TestSDRPacingDiagnosticConfiguration(t *testing.T) {
	for _, text := range []string{"43m45s", "25m20s"} {
		t.Run(text, func(t *testing.T) {
			interval, err := time.ParseDuration(text)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := testSDRPacer(t, interval, true)
			var got sdrPacingEvent
			p.observer.sink = func(e sdrPacingEvent) { got = e }
			p.logConfiguration(interval, true)
			f := pacingFields(got)
			if got.kind != sdrPacingConfigured || f["pacing_enabled"] != true || f["min_start_interval"] != interval.String() || f["start_jitter"] != true || f["jitter_offset"] != p.offset.String() {
				t.Fatalf("incomplete startup configuration: %v", f)
			}
		})
	}
	// The zero-interval startup event distinguishes the configured flag from
	// effective pacing; it never implies a phase gate when pacing is disabled.
	f := pacingFields(sdrPacingEvent{kind: sdrPacingConfigured, snapshot: sdrPacingSnapshot{jitter: true}})
	if f["pacing_enabled"] != false || f["start_jitter"] != true || f["min_start_interval"] != "0s" {
		t.Fatalf("disabled configuration misreported: %v", f)
	}
}

func TestSDRPacingProductionLogPath(t *testing.T) {
	// This non-parallel test captures the real logger, not the pacer test sink.
	// Restore the package pointer without changing the shared logger's core.
	previous := log
	copyLogger := *log
	core, entries := observer.New(zapcore.InfoLevel)
	copyLogger.SugaredLogger = *zap.New(core).Sugar()
	log = &copyLogger
	t.Cleanup(func() { log = previous })
	var s SDRTask
	if err := s.ConfigureStartPacing(time.Minute, false, "", ""); err != nil {
		t.Fatal(err)
	}
	start, cancel, ok := s.ReserveTaskStart(42)
	if !ok {
		t.Fatal("first reservation refused")
	}
	defer cancel()
	if entries.Len() != 1 {
		t.Fatal("startup did not log exactly once, or reservation logged a start")
	}
	if err := start(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitSDREntryLog(t, s.startPacer)
	if _, _, ok := s.ReserveTaskStart(43); ok {
		t.Fatal("interval not enforced")
	}
	var disabled SDRTask
	if err := disabled.ConfigureStartPacing(0, true, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := disabled.ConfigureStartPacing(-time.Minute, false, "", ""); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	got := entries.All()
	want := []string{sdrPacingConfigured, sdrPacingStarted, sdrPacingBlocked, sdrPacingConfigured}
	if len(got) != len(want) {
		t.Fatalf("wrong production events: %+v", got)
	}
	for i, message := range want {
		if got[i].Message != message {
			t.Fatalf("event %d = %q; want %q", i, got[i].Message, message)
		}
	}
	if got[3].ContextMap()["pacing_enabled"] != false || got[3].ContextMap()["start_jitter"] != true {
		t.Fatal("disabled startup did not preserve the configured flag")
	}
}

func TestSDRPacingBlockedDiagnostics(t *testing.T) {
	for _, reason := range []string{"reservation pending", "min start interval", "start jitter phase"} {
		t.Run(reason, func(t *testing.T) {
			p, n := testSDRPacer(t, 10*time.Minute, reason == "start jitter phase")
			p.offset = 7 * time.Second
			var events []sdrPacingEvent
			p.observer.sink = func(e sdrPacingEvent) { events = append(events, e) }
			s := &SDRTask{startPacer: p}
			if reason != "start jitter phase" {
				start, cancel, ok := s.ReserveTaskStart(1)
				if !ok {
					t.Fatal("first reservation refused")
				}
				defer cancel()
				if reason == "min start interval" {
					if err := start(context.Background()); err != nil {
						t.Fatal(err)
					}
					awaitSDREntryLog(t, p)
				}
			}
			events = nil
			if _, _, ok := s.ReserveTaskStart(2); ok {
				t.Fatal("blocked fixture accepted")
			}
			before := pacingState(p)
			for range 100 {
				s.ReserveTaskStart(2)
			}
			if len(events) != 1 || events[0].kind != sdrPacingBlocked || events[0].snapshot.reason != reason {
				t.Fatalf("expected one sampled %q event, got %+v", reason, events)
			}
			f := pacingFields(events[0])
			if reason == "reservation pending" {
				if f["remaining"] != "unknown" || f["next_start_at"] != "unknown" || f["next_start_known"] != false {
					t.Fatalf("invented a pre-entry reservation deadline: %v", f)
				}
			} else {
				remaining := 10 * time.Minute
				if reason == "start jitter phase" {
					remaining = p.phaseWait
				}
				if f["remaining"] != remaining.String() || f["next_start_at"] != n.wall.Add(remaining).Format(time.RFC3339Nano) || f["next_start_known"] != true {
					t.Fatalf("incorrect wait diagnostic: %v", f)
				}
			}
			// Wall jumps do not bypass log throttling or alter monotonic waits.
			n.wall = n.wall.Add(48 * time.Hour)
			n.elapsed = time.Minute - time.Nanosecond
			s.ReserveTaskStart(2)
			if len(events) != 1 {
				t.Fatal("wall jump or early poll bypassed log throttle")
			}
			n.wall = n.wall.Add(-96 * time.Hour)
			n.elapsed = time.Minute
			s.ReserveTaskStart(2)
			if len(events) != 2 {
				t.Fatal("exact monotonic logging interval did not emit")
			}
			if reason != "reservation pending" {
				if events[1].snapshot.remaining != events[0].snapshot.remaining-time.Minute {
					t.Fatal("remaining time followed wall clock instead of elapsed time")
				}
			}
			// A delayed diagnostic from another goroutine cannot rewind the limiter.
			p.observer.blocked(events[0].snapshot)
			if len(events) != 2 || !reflect.DeepEqual(before, pacingState(p)) {
				t.Fatal("diagnostics changed pacing or replayed a stale event")
			}
		})
	}
}

func TestSDRPacingLogsOnlyCommittedDoEntry(t *testing.T) {
	p, n := testSDRPacer(t, time.Minute, false)
	var committed []sdrPacingEvent
	p.observer.sink = func(e sdrPacingEvent) {
		if e.kind == sdrPacingStarted {
			committed = append(committed, e)
		}
	}
	s := &SDRTask{startPacer: p}
	for range 10 {
		if _, err := s.CanAccept([]harmonytask.TaskID{1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	staleStart, cancel, _ := s.ReserveTaskStart(1)
	cancel()
	if err := staleStart(context.Background()); err == nil {
		t.Fatal("stale token accepted")
	}
	start, cancel, _ := s.ReserveTaskStart(2)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := start(ctx); err != context.Canceled {
		t.Fatalf("cancelled start: %v", err)
	}
	cancel()
	start, cancel, ok := s.ReserveTaskStart(3)
	if !ok || len(committed) != 0 {
		t.Fatal("speculation or aborted reservation logged a committed start")
	}
	token := p.reserved
	if err := start(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitSDREntryLog(t, p)
	cancel() // Execution failure does not undo entry or its diagnostic.
	if err := start(context.Background()); err == nil {
		t.Fatal("double start accepted")
	}
	if len(committed) != 1 || committed[0].taskID != 3 || committed[0].token != token || committed[0].snapshot.observed != *n || committed[0].snapshot.remaining != time.Minute {
		t.Fatalf("wrong committed Do-entry event: %+v", committed)
	}
	if _, _, ok := s.ReserveTaskStart(3); ok {
		t.Fatal("logging refunded a committed interval")
	}
}

func TestSDRPacingLoggingOutsideLocks(t *testing.T) {
	p, _ := testSDRPacer(t, time.Minute, false)
	var blocked atomic.Int32
	p.observer.sink = func(e sdrPacingEvent) {
		// TryLock gives an assertion (not a hung test) if either lock regresses.
		if !p.mu.TryLock() {
			t.Error("logging holds the pacer mutex")
			return
		}
		p.mu.Unlock()
		if !p.observer.mu.TryLock() {
			t.Error("logging holds the diagnostic throttle mutex")
			return
		}
		p.observer.mu.Unlock()
		_ = pacingFields(sdrPacingEvent{kind: e.kind, snapshot: p.snapshot()})
		if e.kind == sdrPacingBlocked {
			blocked.Add(1)
			p.observer.blocked(e.snapshot) // Re-entry is throttled, not deadlocked.
		}
	}
	p.logConfiguration(p.interval, p.jitter)
	token, ok := p.reserve()
	if !ok {
		t.Fatal("first reservation refused")
	}
	p.reserve()
	if err := p.start(context.Background(), token, 1); err != nil {
		t.Fatal(err)
	}
	if blocked.Load() != 1 {
		t.Fatalf("re-entry logged %d blocked events", blocked.Load())
	}
}

func TestSDRPacingConcurrentBlockedLogsAreBounded(t *testing.T) {
	p, _ := testSDRPacer(t, time.Minute, false)
	var logged atomic.Int32
	p.observer.sink = func(e sdrPacingEvent) {
		if e.kind == sdrPacingBlocked {
			logged.Add(1)
		}
	}
	token, _ := p.reserve()
	defer p.cancel(token)
	before := pacingState(p)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if _, ok := p.reserve(); ok {
				t.Error("concurrent contender acquired an occupied reservation")
			}
		})
	}
	wg.Wait()
	if logged.Load() != 1 || !reflect.DeepEqual(before, pacingState(p)) {
		t.Fatalf("concurrent diagnostics changed pacing or emitted %d logs", logged.Load())
	}
}

func TestSDRPacingSlowLoggerDoesNotHoldAdmission(t *testing.T) {
	for _, event := range []string{sdrPacingBlocked, sdrPacingStarted} {
		t.Run(event, func(t *testing.T) {
			p, _ := testSDRPacer(t, time.Minute, false)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			p.observer.sink = func(e sdrPacingEvent) {
				if e.kind == event {
					close(entered)
					<-release
				}
			}
			s := &SDRTask{startPacer: p}
			start, cancel, _ := s.ReserveTaskStart(1)
			go func() {
				defer close(done)
				if event == sdrPacingStarted {
					if err := start(context.Background()); err != nil {
						t.Error(err)
					}
				} else {
					s.ReserveTaskStart(2)
				}
			}()
			t.Cleanup(func() {
				unblock.Do(func() { close(release) })
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("logging participant did not exit")
				}
				awaitSDREntryLog(t, p)
			})
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("logging sink not reached")
			}
			// No wait for the logger while withholding its release on failure.
			if !p.mu.TryLock() {
				t.Fatal("slow logger holds admission mutex")
			}
			p.mu.Unlock()
			cancel()
			_, cancelNext, ok := s.ReserveTaskStart(3)
			if event == sdrPacingStarted && ok {
				cancelNext()
				t.Fatal("committed interval was not visible while logger was blocked")
			}
			if event == sdrPacingBlocked {
				if !ok {
					t.Fatal("cancellation could not release reservation during logging")
				}
				cancelNext()
			}
		})
	}
}
