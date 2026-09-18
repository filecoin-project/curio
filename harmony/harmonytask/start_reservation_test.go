package harmonytask

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

type startReservationStub struct {
	TaskInterface
	reserve func(TaskID) (func(context.Context) error, func(), bool)
}

func (s startReservationStub) ReserveTaskStart(id TaskID) (func(context.Context) error, func(), bool) {
	return s.reserve(id)
}

func TestStartReservationBatchAndDisabled(t *testing.T) {
	ids := []TaskID{12, 23, 34}
	for _, paced := range []bool{false, true} {
		calls := 0
		stub := startReservationStub{reserve: func(id TaskID) (func(context.Context) error, func(), bool) {
			calls++
			if id != ids[0] {
				t.Fatal("candidate order changed")
			}
			if !paced {
				return nil, nil, true
			}
			return func(context.Context) error { return nil }, func() {}, true
		}}
		selected, r := reserveTaskStart(stub, ids)
		if calls != 1 || (paced && (len(selected) != 1 || r == nil)) || (!paced && (len(selected) != 3 || r != nil)) {
			t.Fatalf("paced=%v selected=%v reservation=%v", paced, selected, r)
		}
	}
}

func TestStartReservationExecutionAndCancellation(t *testing.T) {
	for _, mode := range []string{"success", "do-error", "cancelled", "start-error"} {
		t.Run(mode, func(t *testing.T) {
			started, cancelled, ran := 0, 0, 0
			boom := errors.New("synthetic failure")
			stub := startReservationStub{reserve: func(TaskID) (func(context.Context) error, func(), bool) {
				return func(context.Context) error {
					started++
					if mode == "start-error" {
						return boom
					}
					return nil
				}, func() { cancelled++ }, true
			}}
			_, r := reserveTaskStart(stub, []TaskID{1})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			store := newMemoryAttemptStore()
			_, tokens := prepareTaskAttempts(context.Background(), store, []TaskID{1})
			done, err := withStartReservationCleanup(r, func() (bool, error) {
				return runWithAttemptStart(ctx, store, 1, tokens[1], r.start, time.Now, func(time.Time) {}, func() (bool, error) {
					ran++
					if started != 1 {
						t.Fatal("Do entered before reservation commit")
					}
					if mode == "do-error" {
						return false, boom
					}
					return true, nil
				})
			})
			if cancelled != 1 || done != (mode == "success") || (err == nil) != (mode == "success") {
				t.Fatalf("done=%v error=%v cancelled=%d", done, err, cancelled)
			}
			if (mode == "cancelled" && started != 0) || ((mode == "cancelled" || mode == "start-error") && ran != 0) {
				t.Fatal("pre-execution failure entered task")
			}
		})
	}
}

func TestStartReservationCleanupPrecedesCompletion(t *testing.T) {
	for _, mode := range []string{"cancelled-before-entry", "panic-before-entry"} {
		t.Run(mode, func(t *testing.T) {
			var reserved atomic.Bool
			reserved.Store(true)
			r := &taskStartReservation{
				start:  func(context.Context) error { panic("synthetic entry panic") },
				cancel: func() { reserved.Store(false) },
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled-before-entry" {
				cancel()
			}
			store := newMemoryAttemptStore()
			_, tokens := prepareTaskAttempts(context.Background(), store, []TaskID{1})
			type completionState struct {
				reserved bool
				panic    any
				err      error
				ran      bool
			}
			completion := make(chan completionState, 1)
			releaseCompletion := make(chan struct{})
			joined := make(chan struct{})
			watchdog, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			defer func() {
				close(releaseCompletion)
				select {
				case <-joined:
				case <-watchdog.Done():
					t.Error("test-owned completion participant did not exit")
				}
			}()
			go func() {
				defer close(joined)
				// Match the production safety-net defer, registered before
				// completion. It cannot free the token while completion waits.
				defer r.cancel()
				var result completionState
				defer func() {
					result.panic = recover()
					result.reserved = reserved.Load()
					completion <- result
					select {
					case <-releaseCompletion:
					case <-watchdog.Done():
					}
				}()
				_, result.err = withStartReservationCleanup(r, func() (bool, error) {
					return runWithAttemptStart(ctx, store, 1, tokens[1], r.start, time.Now, func(time.Time) {}, func() (bool, error) {
						result.ran = true
						return true, nil
					})
				})
			}()
			select {
			case result := <-completion:
				if result.reserved {
					t.Fatal("reservation still held when completion persistence begins")
				}
				if result.ran || (mode == "cancelled-before-entry" && (!errors.Is(result.err, context.Canceled) || result.panic != nil)) ||
					(mode == "panic-before-entry" && result.panic != "synthetic entry panic") {
					t.Fatalf("unexpected entry outcome: %+v", result)
				}
			case <-watchdog.Done():
				t.Fatal("entry did not reach bounded completion wait")
			}
			if len(store.recordCalled) != 0 {
				t.Fatal("unstarted task recorded a live attempt")
			}
		})
	}
}

func TestStartReservationRealSchedulerCacheRecoveryAndResources(t *testing.T) {
	for _, source := range []string{workSourcePoller, workSourceRecover, workSourcePreempt} {
		for _, hasResources := range []bool{false, true} {
			t.Run(source+"/resources="+map[bool]string{false: "none", true: "available"}[hasResources], func(t *testing.T) {
				reserved := 0
				accept := &stubAcceptTask{}
				stub := startReservationStub{TaskInterface: accept, reserve: func(TaskID) (func(context.Context) error, func(), bool) {
					reserved++
					return nil, nil, false
				}}
				cpu := 0
				if hasResources {
					cpu = 2
				}
				h := &taskTypeHandler{
					TaskInterface:   stub,
					TaskTypeDetails: TaskTypeDetails{Name: "PacedStub", Max: taskhelp.Max(10), Cost: resources.Resources{Cpu: 1}},
					TaskEngine:      &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), reg: &resources.Reg{Resources: resources.Resources{Cpu: cpu}}}},
					running:         runregistry.New(), accept: acceptcache.New(time.Hour), storageFailures: map[TaskID]time.Time{},
				}
				h.accept.Add([]int64{1, 2})
				// No DB is supplied: an accidental claim or recovered dispatch is
				// a failure, not a connection to a fallback test target.
				if h.considerWork(source, []task{{ID: 1}, {ID: 2}}, eventEmitter{}) {
					t.Fatal("pacing refusal was bypassed")
				}
				if (hasResources && reserved != 1) || (!hasResources && reserved != 0) || accept.canAcceptCalls.Load() != 0 {
					t.Fatalf("reserved=%d CanAccept calls=%d", reserved, accept.canAcceptCalls.Load())
				}
			})
		}
	}
}

type startReservationStorage struct{ claims int }

func (*startReservationStorage) HasCapacity() bool { return true }

func (s *startReservationStorage) Claim(int) (func() error, error) {
	s.claims++
	return nil, errors.New("synthetic storage claim failure")
}

func TestStartReservationRealClaimAndStorageFailures(t *testing.T) {
	for _, mode := range []string{"claim-lost", "claim-error", "context-cancelled", "attempt-prepare-error", "storage-error", "release-error", "recovery-storage-error"} {
		t.Run(mode, func(t *testing.T) {
			reserved, started, cancelled, claims, releases := 0, 0, 0, 0, 0
			storage := &startReservationStorage{}
			stub := startReservationStub{TaskInterface: &stubAcceptTask{}, reserve: func(TaskID) (func(context.Context) error, func(), bool) {
				reserved++
				return func(context.Context) error { started++; return nil }, func() { cancelled++ }, true
			}}
			h := &taskTypeHandler{
				TaskInterface:   stub,
				TaskTypeDetails: TaskTypeDetails{Name: "PacedStub", Max: taskhelp.Max(10), Cost: resources.Resources{Cpu: 1, Storage: storage}},
				TaskEngine:      &TaskEngine{cfg: taskEngineConfig{ctx: context.Background(), reg: &resources.Reg{Resources: resources.Resources{Cpu: 2}}}},
				running:         runregistry.New(), accept: acceptcache.New(time.Hour), storageFailures: map[TaskID]time.Time{},
			}
			source := workSourcePoller
			if mode == "recovery-storage-error" {
				source = workSourceRecover
			}
			store := newMemoryAttemptStore()
			if mode == "attempt-prepare-error" {
				store.prepareErr = errors.New("synthetic preparation failure")
			}
			ok := h.considerWorkWithOwnership(source, []task{{ID: 1}, {ID: 2}}, eventEmitter{},
				func(ids []TaskID, _ int) ([]TaskID, error) {
					claims++
					if len(ids) != 1 || ids[0] != 1 {
						t.Fatalf("unpaced claim batch: %v", ids)
					}
					switch mode {
					case "claim-lost":
						return nil, nil
					case "claim-error":
						return nil, errors.New("synthetic claim error")
					case "context-cancelled":
						return nil, context.Canceled
					default:
						return ids, nil
					}
				}, func(ids []TaskID, tokens map[TaskID]string) error {
					releases++
					if len(ids) != 1 || ids[0] != 1 {
						t.Fatalf("wrong ownership release: %v", ids)
					}
					if tokens[ids[0]] == "" {
						t.Fatal("storage ownership release lost the prepared attempt token")
					}
					if mode == "release-error" {
						return errors.New("synthetic ownership release error")
					}
					return nil
				}, store)
			settleAdmissions(t, h)
			pending := mode == "attempt-prepare-error" || mode == "storage-error" || mode == "release-error" || mode == "recovery-storage-error"
			if ok != pending || reserved != 1 || started != 0 || cancelled != 1 || h.Max.Active() != 0 {
				t.Fatalf("accepted=%v reserved=%d started=%d cancelled=%d active=%d", ok, reserved, started, cancelled, h.Max.Active())
			}
			storagePhase := mode == "storage-error" || mode == "release-error" || mode == "recovery-storage-error"
			if (storagePhase && (storage.claims != 1 || releases != 1)) || (!storagePhase && (storage.claims != 0 || releases != 0)) {
				t.Fatalf("storage claims=%d ownership releases=%d", storage.claims, releases)
			}
			if claims != 1 {
				t.Fatalf("unexpected claim calls=%d for %s", claims, source)
			}
		})
	}
}

func TestStartReservationProductionCallSites(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "task_type_handler.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reserve, claim, storage, run, cleanup, cleanupEnd token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			switch fun.Name {
			case "reserveTaskStart":
				reserve = call.Pos()
			case "claim":
				claim = call.Pos()
			case "runWithAttemptStart":
				run = call.Pos()
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == "releaseLocal" {
				cleanup = call.Pos()
			}
			if fun.Sel.Name == "recordCompletion" {
				cleanupEnd = call.Pos()
			}
			if fun.Sel.Name == "beginAdmission" {
				storage = call.Pos()
			}
		}
		return true
	})
	if reserve == token.NoPos || reserve >= claim || claim >= storage || storage >= run {
		t.Fatalf("production reserve/claim/admission/Do path not protected: %v %v %v %v", reserve, claim, storage, run)
	}
	if cleanup == token.NoPos || cleanup >= cleanupEnd || cleanupEnd >= run {
		t.Fatal("production defer must release the admission before completion persistence")
	}
}
