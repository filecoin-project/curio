package harmonytask

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type memoryAttemptStore struct {
	mu           sync.Mutex
	tokens       map[TaskID]string
	starts       map[TaskID]time.Time
	released     []TaskID
	prepareErr   error
	recordErr    error
	recordCalled chan struct{}
	blockWriter  bool
	panicWriter  bool
	writerExited chan struct{}
}

func newMemoryAttemptStore() *memoryAttemptStore {
	return &memoryAttemptStore{tokens: map[TaskID]string{}, starts: map[TaskID]time.Time{}, recordCalled: make(chan struct{}, 1024), writerExited: make(chan struct{}, 1024)}
}
func (s *memoryAttemptStore) prepare(ctx context.Context, id TaskID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.prepareErr != nil {
		return s.prepareErr
	}
	s.tokens[id] = token
	delete(s.starts, id)
	return nil
}
func (s *memoryAttemptStore) record(ctx context.Context, id TaskID, token string, start time.Time) (bool, error) {
	defer func() { s.writerExited <- struct{}{} }()
	if s.panicWriter {
		panic("synthetic writer panic")
	}
	if s.blockWriter {
		s.recordCalled <- struct{}{}
		<-ctx.Done()
		return false, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { s.recordCalled <- struct{}{} }()
	if s.recordErr != nil {
		return false, s.recordErr
	}
	if s.tokens[id] != token || !s.starts[id].IsZero() {
		return false, nil
	}
	s.starts[id] = start
	return true, nil
}
func (s *memoryAttemptStore) releaseUnstarted(_ context.Context, id TaskID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, id)
	delete(s.tokens, id)
	delete(s.starts, id)
	return nil
}

func TestAttemptPreparationIsFreshOnSameOwnerRecovery(t *testing.T) {
	s := newMemoryAttemptStore()
	ids, first := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	if len(ids) != 1 || first[1] == "" {
		t.Fatalf("first=%v", first)
	}
	s.starts[1] = time.Unix(10, 0)
	_, second := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	if first[1] == second[1] || !s.starts[1].IsZero() {
		t.Fatal("recovery retained the prior attempt")
	}
	if ok, err := s.record(context.Background(), 1, first[1], time.Unix(20, 0)); err != nil || ok {
		t.Fatalf("stale token record=%v %v", ok, err)
	}
}

func TestAttemptPreparationFailureReleasesWithoutStart(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		s := newMemoryAttemptStore()
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		} else {
			s.prepareErr = errors.New("synthetic unavailable")
		}
		ids, tokens := prepareTaskAttempts(ctx, s, []TaskID{1, 2})
		cancel()
		if len(ids) != 0 || len(tokens) != 0 || len(s.released) != 2 || len(s.starts) != 0 {
			t.Fatalf("ids=%v tokens=%v released=%v starts=%v", ids, tokens, s.released, s.starts)
		}
	}
}

func TestAttemptPreparationFailureDoesNotDispatchFromScheduler(t *testing.T) {
	for _, source := range []string{workSourcePoller, workSourceRecover, workSourcePreempt} {
		t.Run(source, func(t *testing.T) {
			store := newMemoryAttemptStore()
			store.prepareErr = errors.New("synthetic preparation failure")
			h := &taskTypeHandler{
				TaskInterface:   &stubAcceptTask{},
				TaskTypeDetails: TaskTypeDetails{Name: "Synthetic", Max: taskhelp.Max(2), Cost: resources.Resources{Cpu: 1}},
				TaskEngine:      &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), reg: &resources.Reg{Resources: resources.Resources{Cpu: 2}}}},
				running:         runregistry.New(), accept: acceptcache.New(time.Hour), storageFailures: map[TaskID]time.Time{},
			}
			accepted := h.considerWorkWithOwnership(source, []task{{ID: 1}}, eventEmitter{},
				func(ids []TaskID, _ int) ([]TaskID, error) { return ids, nil },
				func([]TaskID, map[TaskID]string) error { t.Fatal("unexpected storage ownership release"); return nil }, store)
			settleAdmissions(t, h)
			if !accepted || h.Max.Active() != 0 || len(store.released) != 1 || len(store.starts) != 0 {
				t.Fatalf("accepted=%v active=%d released=%v starts=%v", accepted, h.Max.Active(), store.released, store.starts)
			}
		})
	}
}

func TestAttemptStartUsesDoEntryForLiveAndHistory(t *testing.T) {
	s := newMemoryAttemptStore()
	_, tokens := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	claimed := time.Unix(100, 0)
	entry := claimed.Add(10 * time.Minute)
	historyStart := claimed
	done, err := runWithAttemptStart(context.Background(), s, 1, tokens[1], nil, func() time.Time { return entry }, func(start time.Time) { historyStart = start }, func() (bool, error) {
		if !historyStart.Equal(entry) {
			t.Fatal("History still uses pre-entry start")
		}
		select {
		case <-s.recordCalled:
		case <-time.After(time.Second):
			t.Fatal("writer not called")
		}
		return true, nil
	})
	if err != nil || !done || !s.starts[1].Equal(entry) || !historyStart.Equal(entry) {
		t.Fatalf("done=%v err=%v live=%v history=%v", done, err, s.starts[1], historyStart)
	}
}

func TestAttemptStartCancellationAndReservationFailure(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		s := newMemoryAttemptStore()
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		calls := 0
		_, err := runWithAttemptStart(ctx, s, 1, "token", func(context.Context) error { return errors.New("reservation failed") }, time.Now, func(time.Time) { calls++ }, func() (bool, error) { calls++; return true, nil })
		cancel()
		if err == nil || calls != 0 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
		select {
		case <-s.recordCalled:
			t.Fatal("unstarted attempt written")
		default:
		}
	}
}

func TestAttemptStartHasNoCancellationGapAfterReservation(t *testing.T) {
	s := newMemoryAttemptStore()
	_, tokens := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := false
	done, err := runWithAttemptStart(ctx, s, 1, tokens[1], func(context.Context) error { cancel(); return nil }, time.Now, func(time.Time) {}, func() (bool, error) { entered = true; return true, nil })
	if !done || err != nil || !entered {
		t.Fatalf("committed reservation did not enter Do: %v %v", done, err)
	}
}

func TestAttemptExecutionFailureDoesNotEraseStart(t *testing.T) {
	s := newMemoryAttemptStore()
	_, tokens := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	entry := time.Unix(100, 0)
	_, err := runWithAttemptStart(context.Background(), s, 1, tokens[1], nil, func() time.Time { return entry }, func(time.Time) {}, func() (bool, error) { <-s.recordCalled; return false, errors.New("task failed") })
	if err == nil || !s.starts[1].Equal(entry) {
		t.Fatalf("failed attempt lost start: %v %v", s.starts, err)
	}
}

func TestAttemptTelemetryErrorDoesNotFabricateSuccess(t *testing.T) {
	s := newMemoryAttemptStore()
	s.recordErr = errors.New("synthetic write failure")
	_, tokens := prepareTaskAttempts(context.Background(), s, []TaskID{1})
	done, err := runWithAttemptStart(context.Background(), s, 1, tokens[1], nil, time.Now, func(time.Time) {}, func() (bool, error) { <-s.recordCalled; return true, nil })
	if !done || err != nil || len(s.starts) != 0 {
		t.Fatalf("execution=%v %v telemetry=%v", done, err, s.starts)
	}
}

func TestAttemptWriterJoinedOnPanic(t *testing.T) {
	s := newMemoryAttemptStore()
	s.blockWriter = true
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer func() { _ = recover() }()
		_, _ = runWithAttemptStart(context.Background(), s, 1, "token", nil, time.Now, func(time.Time) {}, func() (bool, error) { <-s.recordCalled; panic("synthetic panic") })
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("writer not canceled/joined")
	}
	select {
	case <-s.writerExited:
	default:
		t.Fatal("participant survived wrapper")
	}
}

func TestAttemptWriterPanicDoesNotEscapeTelemetry(t *testing.T) {
	s := newMemoryAttemptStore()
	s.panicWriter = true
	done, err := runWithAttemptStart(context.Background(), s, 1, "token", nil, time.Now, func(time.Time) {}, func() (bool, error) {
		select {
		case <-s.writerExited:
		case <-time.After(time.Second):
			t.Fatal("writer did not exit")
		}
		return true, nil
	})
	if !done || err != nil || len(s.starts) != 0 {
		t.Fatalf("execution=%v %v telemetry=%v", done, err, s.starts)
	}
}

func TestAttemptProductionCallSiteAndSQLGuard(t *testing.T) {
	data, err := os.ReadFile("task_type_handler.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	admission, err := os.ReadFile("admission.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "h.beginAdmission(") || !strings.Contains(source, "runWithAttemptStart(") || !strings.Contains(source, "workStart = start") ||
		!strings.Contains(string(admission), "a.store.prepare(") || !strings.Contains(string(admission), "h.Cost.Claim(") || !strings.Contains(string(admission), "h.dispatchAdmission(a)") {
		t.Fatal("production does not use the checked attempt boundary")
	}
	for _, s := range []string{"owner_id=$3", "attempt_id=$4", "attempt_started_at IS NULL", "attempt_start_source='prepared'"} {
		if !strings.Contains(RECORD_TASK_ATTEMPT_START, s) {
			t.Fatalf("missing CAS %s", s)
		}
	}
	if strings.Contains(PREPARE_TASK_ATTEMPT, "work_start=") {
		t.Fatal("ownership/history semantics conflated")
	}
}
