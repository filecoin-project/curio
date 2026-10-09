package harmonytask

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestPollSnapshotFilterFailureIsTypeLocal(t *testing.T) {
	for _, sdrFirst := range []bool{false, true} {
		sdr, _, _, _ := newAdmissionFixture(t, func(context.Context, []TaskID) ([]TaskID, error) {
			return nil, errors.New("synthetic SDR query failure")
		})
		sdr.Name = "SDR"
		other, _, _, _ := newAdmissionFixture(t, func(_ context.Context, ids []TaskID) ([]TaskID, error) { return ids, nil })
		other.Name = "Other"
		other.TaskInterface = &stubAcceptTask{} // No SDR-specific filter/reservation.
		handlers := []*taskTypeHandler{other, sdr}
		if sdrFirst {
			handlers = []*taskTypeHandler{sdr, other}
		}
		e := &TaskEngine{cfg: taskEngineConfig{ctx: context.Background()}, handlers: handlers}
		oldSDR := &taskSchedule{hasID: map[TaskID]task{1: {ID: 1}}, choked: true}
		available := map[string]*taskSchedule{"SDR": oldSDR, "Other": {hasID: map[TaskID]task{}}}
		for pass := 0; pass < 3; pass++ {
			id := TaskID(100 + pass)
			snapshot := e.pollAllTaskTypesWithQuery(func(names []string) ([]polledTask, error) {
				if len(names) != 2 {
					t.Fatal("common query lost a registered type")
				}
				return []polledTask{{ID: 2, Name: "SDR"}, {ID: id, Name: "Other"}}, nil
			})
			if _, present := snapshot["SDR"]; present {
				t.Fatal("failed type must be omitted, not emptied")
			}
			if got := snapshot["Other"]; len(got) != 1 || got[0].ID != id {
				t.Fatalf("SDR-only error discarded unrelated new candidate: got %v, want %d", got, id)
			}
			applyDBTaskSnapshot(available, snapshot)
			if available["SDR"] != oldSDR || !oldSDR.choked || len(oldSDR.hasID) != 1 {
				t.Fatal("failed type snapshot was replaced")
			}
			if len(available["Other"].hasID) != 1 || available["Other"].hasID[id].ID != id {
				t.Fatal("successful type was not refreshed")
			}
			claims := 0
			accepted := other.considerWorkWithOwnership(workSourcePoller, []task{available["Other"].hasID[id]}, eventEmitter{},
				func(ids []TaskID, _ int) ([]TaskID, error) {
					claims++
					if !reflect.DeepEqual(ids, []TaskID{id}) {
						t.Fatal(ids)
					}
					return nil, nil
				},
				func([]TaskID, map[TaskID]string) error { t.Fatal("lost claim cannot release an owner"); return nil }, newMemoryAttemptStore())
			if accepted || claims != 1 {
				t.Fatal("unrelated candidate did not reach actual admission/claim boundary")
			}
			sdr.accept.Add([]int64{1})
			if sdr.considerWorkWithOwnership(workSourcePoller, []task{oldSDR.hasID[1]}, eventEmitter{},
				func([]TaskID, int) ([]TaskID, error) {
					t.Fatal("stale SDR cache bypassed failed readiness query")
					return nil, nil
				},
				func([]TaskID, map[TaskID]string) error { t.Fatal("unexpected release"); return nil }, newMemoryAttemptStore()) {
				t.Fatal("stale SDR dispatched")
			}
		}
	}
}

func TestPollSnapshotSuccessfulEmptyClearsOnlyItsType(t *testing.T) {
	sdr := &taskTypeHandler{TaskInterface: candidateFilterStub{filter: func(context.Context, []TaskID) ([]TaskID, error) { return nil, nil }}, TaskTypeDetails: TaskTypeDetails{Name: "SDR"}}
	e := &TaskEngine{cfg: taskEngineConfig{ctx: context.Background()}, handlers: []*taskTypeHandler{sdr}}
	oldOther := &taskSchedule{hasID: map[TaskID]task{9: {ID: 9}}}
	for _, rows := range [][]polledTask{nil, {{ID: 1, Name: "SDR"}}} {
		available := map[string]*taskSchedule{"SDR": {hasID: map[TaskID]task{1: {ID: 1}}, choked: true}, "Other": oldOther}
		snapshot := e.pollAllTaskTypesWithQuery(func([]string) ([]polledTask, error) { return rows, nil })
		if got, present := snapshot["SDR"]; !present || len(got) != 0 {
			t.Fatal("successful empty result must be explicit")
		}
		applyDBTaskSnapshot(available, snapshot)
		if len(available["SDR"].hasID) != 0 || available["SDR"].choked || available["Other"] != oldOther {
			t.Fatal("empty snapshot application corrupted state")
		}
	}
}

func TestPollSnapshotCommonQueryFailurePreservesAll(t *testing.T) {
	filters := 0
	h := &taskTypeHandler{TaskInterface: candidateFilterStub{filter: func(context.Context, []TaskID) ([]TaskID, error) { filters++; return nil, nil }}, TaskTypeDetails: TaskTypeDetails{Name: "SDR"}}
	e := &TaskEngine{cfg: taskEngineConfig{ctx: context.Background()}, handlers: []*taskTypeHandler{h}}
	old := &taskSchedule{hasID: map[TaskID]task{1: {ID: 1}}, choked: true}
	available := map[string]*taskSchedule{"SDR": old}
	for _, cause := range []error{errors.New("common SELECT failed"), context.Canceled, context.DeadlineExceeded} {
		snapshot := e.pollAllTaskTypesWithQuery(func([]string) ([]polledTask, error) { return []polledTask{{ID: 2, Name: "SDR"}}, cause })
		if snapshot != nil || filters != 0 {
			t.Fatal("failed common query became a partial success")
		}
		applyDBTaskSnapshot(available, snapshot)
		if available["SDR"] != old {
			t.Fatal("common query failure replaced state")
		}
	}
}

func TestPollSnapshotProductionConnections(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "scheduler.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	querySeam, querySQL, applied := 0, 0, 0
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "pollAllTaskTypes" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok {
						if s.Sel.Name == "pollAllTaskTypesWithQuery" {
							querySeam++
						}
						if s.Sel.Name == "Select" {
							querySQL++
						}
					}
				}
				return true
			})
		}
		if fn.Name.Name == "runScheduler" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				c, ok := n.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, expr := range c.List {
					if id, ok := expr.(*ast.Ident); ok && id.Name == "schedulerSourceDBPoll" {
						for _, stmt := range c.Body {
							ast.Inspect(stmt, func(n ast.Node) bool {
								if call, ok := n.(*ast.CallExpr); ok {
									if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "applyDBTaskSnapshot" {
										if len(call.Args) != 2 {
											t.Fatal("snapshot disconnected")
										}
										a, ok := call.Args[1].(*ast.SelectorExpr)
										if !ok || a.Sel.Name != "DBTasks" {
											t.Fatal("not applying actual poll event")
										}
										applied++
									}
								}
								return true
							})
						}
					}
				}
				return true
			})
		}
	}
	if querySeam != 1 || querySQL != 1 || applied != 1 {
		t.Fatalf("production connections query=%d SQL=%d apply=%d", querySeam, querySQL, applied)
	}
}
