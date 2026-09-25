package seal

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

func TestSDRCandidateBulkRead(t *testing.T) {
	ids := make([]harmonytask.TaskID, 31258)
	for i := range ids {
		ids[i] = harmonytask.TaskID(i + 1)
	}
	copyIDs := append([]harmonytask.TaskID(nil), ids...)
	calls := 0
	got, err := filterSDRCandidates(context.Background(), func(_ context.Context, out interface{}, args ...interface{}) error {
		calls++
		if len(args) != 1 || !reflect.DeepEqual(args[0], ids) {
			t.Fatal("not one task-ID array")
		}
		*out.(*[]harmonytask.TaskID) = []harmonytask.TaskID{31258}
		return nil
	}, ids)
	if err != nil || !reflect.DeepEqual(got, []harmonytask.TaskID{31258}) || calls != 1 || !reflect.DeepEqual(ids, copyIDs) {
		t.Fatal(got, err, calls)
	}
	_, err = filterSDRCandidates(context.Background(), func(context.Context, interface{}, ...interface{}) error { t.Fatal("empty input queried"); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSDRCandidateQueryErrorsAreNotMissingWork(t *testing.T) {
	for _, cause := range []error{errors.New("synthetic SQL unavailable"), context.Canceled, context.DeadlineExceeded} {
		got, err := filterSDRCandidates(context.Background(), func(_ context.Context, out interface{}, _ ...interface{}) error {
			*out.(*[]harmonytask.TaskID) = []harmonytask.TaskID{1}
			return cause
		}, []harmonytask.TaskID{1})
		if len(got) != 0 || !errors.Is(err, cause) || errors.Is(err, errSDRTaskNotReady) {
			t.Fatal(got, err)
		}
		_, err = loadSDRSectorReference(context.Background(), func(context.Context, interface{}, ...interface{}) error { return cause }, 1)
		if !errors.Is(err, cause) || errors.Is(err, errSDRTaskNotReady) {
			t.Fatal("query error misclassified", err)
		}
	}
	for _, cancelDuringRead := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if !cancelDuringRead {
			cancel()
		}
		calls := 0
		_, err := loadSDRSectorReference(ctx, func(context.Context, interface{}, ...interface{}) error { calls++; cancel(); return nil }, 1)
		cancel()
		if !errors.Is(err, context.Canceled) || errors.Is(err, errSDRTaskNotReady) {
			t.Fatal("cancelled zero rows became not-ready", err)
		}
		if !cancelDuringRead && calls != 0 {
			t.Fatal("queried despite cancellation")
		}
	}
}

func TestSDRCandidateReferenceRecheck(t *testing.T) {
	valid := sdrSectorReference{SpID: 1000, SectorNumber: 20, RegSealProof: 8}
	failed, complete := valid, valid
	failed.Failed = true
	complete.AfterSDR = true
	for _, tc := range []struct {
		name  string
		rows  []sdrSectorReference
		ready bool
	}{
		{"missing", nil, false}, {"valid", []sdrSectorReference{valid}, true},
		{"ambiguous", []sdrSectorReference{valid, valid}, false}, {"failed", []sdrSectorReference{failed}, false},
		{"completed", []sdrSectorReference{complete}, false}, {"mixed", []sdrSectorReference{valid, failed}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := loadSDRSectorReference(context.Background(), func(ctx context.Context, out interface{}, args ...interface{}) error {
				if d, ok := ctx.Deadline(); !ok || time.Until(d) > 5*time.Second {
					t.Fatal("unbounded reference recheck")
				}
				if len(args) != 1 || args[0] != harmonytask.TaskID(1) {
					t.Fatal("task identity lost")
				}
				*out.(*[]sdrSectorReference) = tc.rows
				return nil
			}, 1)
			if tc.ready {
				if err != nil || got.SpID != 1000 || got.SectorNumber != 20 || got.RegSealProof != 8 {
					t.Fatal(got, err)
				}
			} else if !errors.Is(err, errSDRTaskNotReady) {
				t.Fatal("unready reference accepted", got, err)
			}
		})
	}
}

func TestSDRCandidateLostReferenceDoesNotConsumePacing(t *testing.T) {
	for _, interval := range []time.Duration{1520 * time.Second, 2625 * time.Second} {
		p, _ := testSDRPacer(t, interval, false)
		s := &SDRTask{startPacer: p}
		// Advisory discovery observed two valid references. The first disappears
		// before the storage reference recheck; only this reservation is canceled.
		_, cancel, ok := s.ReserveTaskStart(1)
		if !ok {
			t.Fatal("reservation denied")
		}
		_, err := loadSDRSectorReference(context.Background(), func(context.Context, interface{}, ...interface{}) error { return nil }, 1)
		if !errors.Is(err, errSDRTaskNotReady) {
			t.Fatal(err)
		}
		cancel()
		if p.started || p.reserved != 0 {
			t.Fatal("reference failure spent interval")
		}
		start, cancelNext, ok := s.ReserveTaskStart(2)
		if !ok {
			t.Fatal("valid following candidate blocked")
		}
		cancel() // stale cleanup cannot release task 2's token
		if err := start(context.Background()); err != nil {
			t.Fatal(err)
		}
		cancelNext()
		if !p.started || p.reserved != 0 {
			t.Fatal("Do-entry did not commit")
		}
		if _, _, ok := s.ReserveTaskStart(2); ok {
			t.Fatal("failure after actual start refunded pacing")
		}
	}
}

func TestSDRCandidateProductionSQLAndCallSites(t *testing.T) {
	// SQL-shape coverage, not SQL execution: no copied eligibility evaluator.
	want := `SELECT task_id_sdr FROM sectors_sdr_pipeline WHERE task_id_sdr = ANY($1::bigint[])
	GROUP BY task_id_sdr HAVING COUNT(*) = 1 AND bool_and(NOT after_sdr AND NOT failed)`
	if strings.Join(strings.Fields(sdrCandidateSQL), " ") != strings.Join(strings.Fields(want), " ") {
		t.Fatal("bulk eligibility SQL changed")
	}
	wantRef := `SELECT sp_id, sector_number, reg_seal_proof, after_sdr, failed FROM sectors_sdr_pipeline WHERE task_id_sdr = $1`
	if strings.Join(strings.Fields(sdrSectorReferenceSQL), " ") != strings.Join(strings.Fields(wantRef), " ") {
		t.Fatal("reference identity/state projection changed")
	}
	for _, tc := range []struct{ file, fn, call string }{
		{"task_sdr.go", "taskToSector", "sectorReference"}, {"task_sdr.go", "Do", "sectorReference"},
		{"sdr_candidates.go", "FilterCandidates", "filterSDRCandidates"}, {"sdr_candidates.go", "sectorReference", "loadSDRSectorReference"},
	} {
		f, err := parser.ParseFile(token.NewFileSet(), tc.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == tc.fn {
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok {
						switch e := c.Fun.(type) {
						case *ast.Ident:
							if e.Name == tc.call {
								calls++
							}
						case *ast.SelectorExpr:
							if e.Sel.Name == tc.call {
								calls++
							}
						}
					}
					return true
				})
			}
		}
		if calls != 1 {
			t.Fatalf("%s does not use tested %s", tc.fn, tc.call)
		}
	}
}
