package seal

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

func testSDRPacer(t *testing.T, interval time.Duration, jitter bool) (*sdrStartPacer, *sdrPacingTime) {
	t.Helper()
	n := &sdrPacingTime{wall: time.Unix(100, 0)}
	p, err := newSDRStartPacer(interval, jitter, "synthetic-instance", func() sdrPacingTime { return *n })
	if err != nil {
		t.Fatal(err)
	}
	return p, n
}

func TestSDRPacingConfiguration(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, time.Minute} {
		p, err := newSDRStartPacer(interval, true, "", nil)
		if interval == 0 {
			if err != nil || p != nil {
				t.Fatalf("zero must disable pacing: %v %v", p, err)
			}
		} else if err == nil {
			t.Fatalf("invalid interval/identity accepted: %s", interval)
		}
	}
	var s SDRTask
	if err := s.ConfigureStartPacing(time.Minute, true, "", "loopback-instance"); err == nil {
		t.Fatal("missing CURIO_NODE_NAME accepted")
	}
	if err := s.ConfigureStartPacing(time.Minute, true, "node", ""); err == nil {
		t.Fatal("missing per-instance identity accepted")
	}
	if err := s.ConfigureStartPacing(0, true, "", ""); err != nil {
		t.Fatal(err)
	}
	start, cancel, ok := s.ReserveTaskStart(1)
	if !ok || start != nil || cancel != nil {
		t.Fatal("default must preserve unpaced scheduler batch")
	}
}

func TestSDRPacingCanAcceptDoesNotConsume(t *testing.T) {
	p, _ := testSDRPacer(t, time.Minute, false)
	s := &SDRTask{startPacer: p}
	for range 100 {
		ids, err := s.CanAccept([]harmonytask.TaskID{1, 2}, nil)
		if err != nil || len(ids) != 2 {
			t.Fatalf("CanAccept must retain ordinary eligibility: %v %v", ids, err)
		}
	}
	if p.started || p.reserved != 0 || p.phaseSet {
		t.Fatal("speculative or cached eligibility consumed pacing state")
	}
}

func TestSDRPacingAbortedClaimsReleaseWithoutConsuming(t *testing.T) {
	for _, reason := range []string{"claim-lost", "claim-error", "storage-error", "pre-dispatch-cancel"} {
		t.Run(reason, func(t *testing.T) {
			p, _ := testSDRPacer(t, time.Minute, false)
			s := &SDRTask{startPacer: p}
			_, cancel, ok := s.ReserveTaskStart(1)
			if !ok {
				t.Fatal("first reservation refused")
			}
			cancel()
			start2, cancel2, ok := s.ReserveTaskStart(2)
			if !ok {
				t.Fatal("aborted claim consumed the interval")
			}
			defer cancel2()
			cancel() // A stale/double release must not clear the newer token.
			if _, _, ok := s.ReserveTaskStart(3); ok {
				t.Fatal("stale release broke a newer reservation")
			}
			if err := start2(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSDRPacingCancelledStartAndStartedFailure(t *testing.T) {
	p, n := testSDRPacer(t, time.Minute, false)
	s := &SDRTask{startPacer: p}
	start, cancel, _ := s.ReserveTaskStart(1)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := start(ctx); err != context.Canceled {
		t.Fatalf("cancelled start: %v", err)
	}
	cancel()
	start, cancel, ok := s.ReserveTaskStart(2)
	if !ok {
		t.Fatal("cancelled execution consumed interval")
	}
	if err := start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancel() // Task failure/completion cleanup must not refund a real Do entry.
	if _, _, ok := s.ReserveTaskStart(2); ok {
		t.Fatal("immediate retry bypassed minimum interval")
	}
	n.elapsed = time.Minute
	if _, cancel, ok := s.ReserveTaskStart(2); !ok {
		t.Fatal("retry at exact interval rejected")
	} else {
		cancel()
	}
}

func TestSDRPacingConcurrentReservations(t *testing.T) {
	p, _ := testSDRPacer(t, time.Minute, false)
	start := make(chan struct{})
	results := make(chan uint64, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			<-start
			if token, ok := p.reserve(); ok {
				results <- token
			}
		})
	}
	close(start)
	joined := make(chan struct{})
	go func() { wg.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("reservation deadlocked; no nested logging/mutex path is permitted")
	}
	close(results)
	if len(results) != 1 {
		t.Fatalf("simultaneous callers reserved %d slots", len(results))
	}
	for token := range results {
		p.cancel(token)
	}
}

func TestSDRPacingPhaseRestartIdleClockAndIntervals(t *testing.T) {
	for _, text := range []string{"43m45s", "25m20s", "1m"} {
		t.Run(text, func(t *testing.T) {
			interval, err := time.ParseDuration(text)
			if err != nil {
				t.Fatal(err)
			}
			p, n := testSDRPacer(t, interval, true)
			p.offset = 7 * time.Second // deterministic alignment, independent of hash collisions
			p.reserve()
			if !p.phaseSet || p.phaseWait < 0 || p.phaseWait >= interval {
				t.Fatalf("invalid first phase: %+v", p)
			}
			wait := p.phaseWait
			n.wall = n.wall.Add(-24 * time.Hour)
			for range 10 {
				p.reserve()
			}
			if p.phaseWait != wait || p.phaseStart != 0 {
				t.Fatal("repeated polling or clock jump moved phase deadline")
			}
			n.elapsed = wait
			token, ok := p.reserve()
			if !ok {
				t.Fatal("monotonic phase wait did not expire")
			}
			if err := p.start(context.Background(), token, 1); err != nil {
				t.Fatal(err)
			}
			n.wall = n.wall.Add(48 * time.Hour)
			n.elapsed += interval - time.Nanosecond
			if _, ok := p.reserve(); ok {
				t.Fatal("wall clock leap bypassed monotonic interval")
			}
			n.elapsed += time.Nanosecond
			token, ok = p.reserve()
			if !ok {
				t.Fatal("exact interval should be eligible")
			}
			p.cancel(token)
			n.elapsed += 3 * interval
			p.reserve()
			if !p.phaseSet {
				t.Fatal("long idle did not re-latch phase")
			}
			restarted, _ := testSDRPacer(t, interval, true)
			if restarted.started || restarted.offset != hashSDROffset(t, interval, "synthetic-instance") {
				t.Fatal("restart must reset local interval but preserve identity phase")
			}
		})
	}
}

func hashSDROffset(t *testing.T, interval time.Duration, identity string) time.Duration {
	t.Helper()
	p, err := newSDRStartPacer(interval, true, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p.offset
}

func TestSDRPacingInstanceIdentityAndConfigRestart(t *testing.T) {
	var a, b, same, changed SDRTask
	for _, c := range []struct {
		task     *SDRTask
		port     string
		interval time.Duration
	}{{&a, "host:12300", time.Minute}, {&same, "host:12300", time.Minute}, {&b, "host:12301", time.Minute}, {&changed, "host:12300", 2 * time.Minute}} {
		if err := c.task.ConfigureStartPacing(c.interval, true, "name", c.port); err != nil {
			t.Fatal(err)
		}
	}
	if a.startPacer.offset != same.startPacer.offset {
		t.Fatal("same instance identity changed phase")
	}
	if a.startPacer.offset == b.startPacer.offset {
		t.Fatal("chosen synthetic same-host instance fixtures unexpectedly collided")
	}
	if changed.startPacer.interval != 2*time.Minute || changed.startPacer.started {
		t.Fatal("new config instance inherited old interval state")
	}
}
