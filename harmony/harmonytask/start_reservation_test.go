package harmonytask

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
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
			done, err := runWithStartReservation(ctx, r, func() (bool, error) {
				ran++
				if started != 1 {
					t.Fatal("Do entered before reservation commit")
				}
				if mode == "do-error" {
					return false, boom
				}
				return true, nil
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
	for _, mode := range []string{"claim-lost", "claim-error", "context-cancelled", "storage-error", "release-error", "recovery-storage-error"} {
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
				}, func(ids []TaskID) error {
					releases++
					if len(ids) != 1 || ids[0] != 1 {
						t.Fatalf("wrong ownership release: %v", ids)
					}
					if mode == "release-error" {
						return errors.New("synthetic ownership release error")
					}
					return nil
				})
			if ok || reserved != 1 || started != 0 || cancelled != 1 || h.Max.Active() != 0 {
				t.Fatalf("accepted=%v reserved=%d started=%d cancelled=%d active=%d", ok, reserved, started, cancelled, h.Max.Active())
			}
			storagePhase := mode == "storage-error" || mode == "release-error" || mode == "recovery-storage-error"
			if (storagePhase && (storage.claims != 1 || releases != 1)) || (!storagePhase && (storage.claims != 0 || releases != 0)) {
				t.Fatalf("storage claims=%d ownership releases=%d", storage.claims, releases)
			}
			if (source == workSourceRecover && claims != 0) || (source != workSourceRecover && claims != 1) {
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
	var reserve, claim, storage, run token.Pos
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
			case "runWithStartReservation":
				run = call.Pos()
			}
		case *ast.SelectorExpr:
			if fun.Sel.Name == "Claim" {
				storage = call.Pos()
			}
		}
		return true
	})
	if reserve == token.NoPos || reserve >= claim || claim >= storage || storage >= run {
		t.Fatalf("production reserve/claim/storage/Do order not protected: %v %v %v %v", reserve, claim, storage, run)
	}
}
