package seal

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSDRStartReadinessIsReadOnly(t *testing.T) {
	if (&SDRTask{}).TaskStartBlocked() {
		t.Fatal("zero/default unexpectedly paced")
	}
	for _, interval := range []time.Duration{2625 * time.Second, 1520 * time.Second} {
		p, n := testSDRPacer(t, interval, true)
		s := &SDRTask{startPacer: p}
		p.observer.sink = func(sdrPacingEvent) { _ = p.snapshot() }
		// No active phase yet: the first real reservation must still choose it.
		before := p.snapshot()
		for range 10 {
			if s.TaskStartBlocked() {
				t.Fatal("unlatched phase treated as known wait")
			}
		}
		if p.phaseSet || p.sequence != 0 || !reflect.DeepEqual(before, p.snapshot()) {
			t.Fatal("peek changed admission state")
		}
		p.phaseSet = true
		p.phaseWait = time.Second
		before = p.snapshot()
		for range 10 {
			if !s.TaskStartBlocked() {
				t.Fatal("latched phase not blocked")
			}
		}
		if !reflect.DeepEqual(before, p.snapshot()) {
			t.Fatal("latched deadline moved")
		}
		n.elapsed = time.Second
		start, cancel, ok := s.ReserveTaskStart(1)
		if !ok {
			t.Fatal("due reservation denied")
		}
		if !s.TaskStartBlocked() {
			t.Fatal("provisional reservation not protected")
		}
		if err := start(context.Background()); err != nil {
			t.Fatal(err)
		}
		awaitSDREntryLog(t, p)
		cancel()
		before = p.snapshot()
		for range 10 {
			if !s.TaskStartBlocked() {
				t.Fatal("committed interval not protected")
			}
		}
		if !reflect.DeepEqual(before, p.snapshot()) {
			t.Fatal("diagnostics changed committed interval")
		}
		n.elapsed += interval
		if s.TaskStartBlocked() {
			t.Fatal("exact interval remains blocked")
		}
	}
}

func TestSDRSectorDiagnosticLookupContext(t *testing.T) {
	for _, mode := range []string{"success", "error", "cancelled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer stop()
			}
			boom := errors.New("synthetic lookup error")
			var queryCtx context.Context
			sid, err := lookupSDRSectorID(ctx, func(c context.Context, sp, sector *uint64) error {
				queryCtx = c
				if d, ok := c.Deadline(); !ok || time.Until(d) > 5*time.Second {
					t.Fatal("unbounded diagnostic lookup")
				}
				if e := c.Err(); e != nil {
					return e
				}
				if mode == "error" {
					return boom
				}
				*sp = 1000
				*sector = 2000
				return nil
			})
			if mode == "success" {
				if err != nil || sid == nil || sid.Miner != 1000 || sid.Number != 2000 {
					t.Fatalf("sector: %v %v", sid, err)
				}
			} else if err == nil || sid != nil {
				t.Fatalf("error fabricated sector: %v %v", sid, err)
			}
			if queryCtx.Err() == nil {
				t.Fatal("lookup child context not cancelled on return")
			}
		})
	}
}
