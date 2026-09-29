package storage_market

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/market/mk20release"
)

func TestSnapshotMK20ReleasePolicy(t *testing.T) {
	tests := []struct {
		name      string
		batch     *config.Dynamic[int]
		maxActive *config.Dynamic[int]
		want      mk20ReleasePolicy
		wantErr   bool
	}{
		{name: "nil values default to zero", want: mk20ReleasePolicy{}},
		{name: "zero values", batch: config.NewDynamic(0), maxActive: config.NewDynamic(0), want: mk20ReleasePolicy{}},
		{name: "positive values", batch: config.NewDynamic(7), maxActive: config.NewDynamic(200), want: mk20ReleasePolicy{batch: 7, maxActive: 200}},
		{name: "negative batch", batch: config.NewDynamic(-1), maxActive: config.NewDynamic(200), wantErr: true},
		{name: "negative active cap", batch: config.NewDynamic(1), maxActive: config.NewDynamic(-1), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := snapshotMK20ReleasePolicy(&config.CurioIngestConfig{
				MK20PipelineInsertBatch:     tc.batch,
				MK20PipelineInsertMaxActive: tc.maxActive,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("policy = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMK20ReleasePolicyIsSnapshotted(t *testing.T) {
	batch := config.NewDynamic(2)
	maxActive := config.NewDynamic(10)
	policy, err := snapshotMK20ReleasePolicy(&config.CurioIngestConfig{
		MK20PipelineInsertBatch:     batch,
		MK20PipelineInsertMaxActive: maxActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	batch.Set(9)
	maxActive.Set(99)
	if policy.batch != 2 || policy.maxActive != 10 {
		t.Fatalf("in-progress policy changed: %+v", policy)
	}
}

func defaultMK20ReleasePassDeps(ids []string, release func(string) (mk20release.Outcome, error), wake *int) mk20ReleasePassDeps {
	return mk20ReleasePassDeps{
		waiting:  func(context.Context) (bool, error) { return true, nil },
		pressure: func(context.Context) (bool, error) { return false, nil },
		active:   func(context.Context) (int64, error) { return 0, nil },
		candidates: func(_ context.Context, after string, limit int) ([]string, error) {
			var out []string
			for _, id := range ids {
				if id > after {
					out = append(out, id)
				}
				if len(out) == limit {
					break
				}
			}
			return out, nil
		},
		release: func(_ context.Context, id string, _ int64) (mk20release.Outcome, error) {
			return release(id)
		},
		wakeDealPoller: func() { *wake++ },
	}
}

func TestMK20ReleasePassEmptyQueueFastPath(t *testing.T) {
	t.Run("default mode avoids an extra probe", func(t *testing.T) {
		wake := 0
		waitingCalls := 0
		candidateCalls := 0
		deps := defaultMK20ReleasePassDeps(nil, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called for an empty queue")
			return "", nil
		}, &wake)
		deps.waiting = func(context.Context) (bool, error) {
			waitingCalls++
			return false, nil
		}
		deps.candidates = func(context.Context, string, int) ([]string, error) {
			candidateCalls++
			return nil, nil
		}

		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{}, deps)
		if err != nil || result.scanned != 0 || waitingCalls != 0 || candidateCalls != 1 || wake != 0 {
			t.Fatalf("result=%+v waitingCalls=%d candidateCalls=%d wake=%d err=%v", result, waitingCalls, candidateCalls, wake, err)
		}
	})

	t.Run("empty queue skips expensive work", func(t *testing.T) {
		calls := map[string]int{}
		deps := mk20ReleasePassDeps{
			waiting: func(context.Context) (bool, error) {
				calls["waiting"]++
				return false, nil
			},
			pressure: func(context.Context) (bool, error) {
				calls["pressure"]++
				return false, nil
			},
			active: func(context.Context) (int64, error) {
				calls["active"]++
				return 0, nil
			},
			candidates: func(context.Context, string, int) ([]string, error) {
				calls["candidates"]++
				return []string{"a"}, nil
			},
			release: func(context.Context, string, int64) (mk20release.Outcome, error) {
				calls["release"]++
				return mk20release.Released, nil
			},
			wakeDealPoller: func() { calls["wake"]++ },
		}

		result, err := runMK20ReleasePass(context.Background(), "cursor", mk20ReleasePolicy{batch: 1, maxActive: 10}, deps)
		if err != nil {
			t.Fatal(err)
		}
		if result.cursor != "" || result.scanned != 0 || result.released != 0 {
			t.Fatalf("result = %+v", result)
		}
		if calls["waiting"] != 1 {
			t.Fatalf("waiting checks = %d, want 1", calls["waiting"])
		}
		for _, name := range []string{"pressure", "active", "candidates", "release", "wake"} {
			if calls[name] != 0 {
				t.Fatalf("%s calls = %d, want 0", name, calls[name])
			}
		}
	})

	t.Run("empty queue check error fails closed", func(t *testing.T) {
		checkErr := errors.New("waiting query")
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called after waiting check error")
			return "", nil
		}, &wake)
		deps.waiting = func(context.Context) (bool, error) { return false, checkErr }
		pressureCalls := 0
		deps.pressure = func(context.Context) (bool, error) {
			pressureCalls++
			return false, nil
		}

		result, err := runMK20ReleasePass(context.Background(), "cursor", mk20ReleasePolicy{batch: 1, maxActive: 10}, deps)
		if !errors.Is(err, checkErr) || result.cursor != "cursor" || result.released != 0 || pressureCalls != 0 || wake != 0 {
			t.Fatalf("result=%+v pressureCalls=%d wake=%d err=%v", result, pressureCalls, wake, err)
		}
	})

	t.Run("nonempty queue still applies pressure and capacity", func(t *testing.T) {
		wake := 0
		calls := map[string]int{}
		var order []string
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) { return "", nil }, &wake)
		deps.waiting = func(context.Context) (bool, error) {
			calls["waiting"]++
			order = append(order, "waiting")
			return true, nil
		}
		deps.pressure = func(context.Context) (bool, error) {
			calls["pressure"]++
			order = append(order, "pressure")
			return false, nil
		}
		deps.active = func(context.Context) (int64, error) {
			calls["active"]++
			order = append(order, "active")
			return 9, nil
		}
		deps.candidates = func(context.Context, string, int) ([]string, error) {
			order = append(order, "candidates")
			return []string{"a"}, nil
		}
		deps.release = func(_ context.Context, id string, maxActive int64) (mk20release.Outcome, error) {
			calls["release"]++
			order = append(order, "release")
			if id != "a" || maxActive != 10 {
				t.Fatalf("release id=%q maxActive=%d, want a/10", id, maxActive)
			}
			return mk20release.AtCapacity, nil
		}

		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{batch: 1, maxActive: 10}, deps)
		if err != nil {
			t.Fatal(err)
		}
		if result.scanned != 1 || result.released != 0 || wake != 0 {
			t.Fatalf("result=%+v wake=%d", result, wake)
		}
		for _, name := range []string{"waiting", "pressure", "active", "release"} {
			if calls[name] != 1 {
				t.Fatalf("%s calls = %d, want 1", name, calls[name])
			}
		}
		if got, want := strings.Join(order, ","), "waiting,pressure,active,candidates,release"; got != want {
			t.Fatalf("call order = %q, want %q", got, want)
		}
	})

	t.Run("candidate removed after observation is a safe skip", func(t *testing.T) {
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			return mk20release.NoLongerWaiting, nil
		}, &wake)
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{batch: 1}, deps)
		if err != nil || result.scanned != 1 || result.noLongerWaiting != 1 || result.released != 0 || wake != 0 {
			t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
		}
	})

	t.Run("nonempty queue wraps an exhausted cursor", func(t *testing.T) {
		wake := 0
		var afters []string
		deps := defaultMK20ReleasePassDeps(nil, func(string) (mk20release.Outcome, error) {
			return mk20release.Released, nil
		}, &wake)
		deps.candidates = func(_ context.Context, after string, _ int) ([]string, error) {
			afters = append(afters, after)
			if after == "z" {
				return nil, nil
			}
			return []string{"a"}, nil
		}

		result, err := runMK20ReleasePass(context.Background(), "z", mk20ReleasePolicy{batch: 1}, deps)
		if err != nil || strings.Join(afters, ",") != "z," || result.cursor != "a" || result.released != 1 || wake != 1 {
			t.Fatalf("afters=%v result=%+v wake=%d err=%v", afters, result, wake, err)
		}
	})

	t.Run("cancellation precedes the advisory queue check", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		wake := 0
		waitingCalls := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called after cancellation")
			return "", nil
		}, &wake)
		deps.waiting = func(context.Context) (bool, error) {
			waitingCalls++
			return true, nil
		}

		result, err := runMK20ReleasePass(ctx, "cursor", mk20ReleasePolicy{batch: 1}, deps)
		if !errors.Is(err, context.Canceled) || result.cursor != "cursor" || waitingCalls != 0 || wake != 0 {
			t.Fatalf("result=%+v waitingCalls=%d wake=%d err=%v", result, waitingCalls, wake, err)
		}
	})
}

func TestMK20PipelineInsertCallSiteWiresWaitingFastPath(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "mk20_release.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var insertCallSite *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "insertDDODealInPipeline" {
			insertCallSite = fn
			break
		}
	}
	if insertCallSite == nil {
		t.Fatal("could not locate production MK20 waiting release call site")
	}

	wired := false
	ast.Inspect(insertCallSite.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !astIdentNamed(call.Fun, "runMK20ReleasePass") || len(call.Args) != 4 {
			return true
		}
		deps, ok := call.Args[3].(*ast.CompositeLit)
		if !ok || !astIdentNamed(deps.Type, "mk20ReleasePassDeps") {
			return true
		}
		for _, element := range deps.Elts {
			field, ok := element.(*ast.KeyValueExpr)
			if !ok || !astIdentNamed(field.Key, "waiting") {
				continue
			}
			callback, ok := field.Value.(*ast.FuncLit)
			if !ok {
				return true
			}
			ast.Inspect(callback.Body, func(node ast.Node) bool {
				probe, ok := node.(*ast.CallExpr)
				if !ok || !astIdentNamed(probe.Fun, "hasMK20WaitingDeals") || len(probe.Args) != 2 {
					return true
				}
				ctxOK := astIdentNamed(probe.Args[0], "ctx")
				db, dbOK := probe.Args[1].(*ast.SelectorExpr)
				wired = ctxOK && dbOK && astIdentNamed(db.X, "d") && db.Sel.Name == "db"
				return !wired
			})
		}
		return !wired
	})
	if !wired {
		t.Fatal("production release pass does not wire its waiting dependency to hasMK20WaitingDeals(ctx, d.db)")
	}
}

func astIdentNamed(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func TestMK20ReleasePassInternalQuantumAndBatchLimit(t *testing.T) {
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = fmt.Sprintf("%03d", i)
	}

	t.Run("zero batch still scans only internal quantum", func(t *testing.T) {
		wake := 0
		calls := 0
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{}, defaultMK20ReleasePassDeps(ids, func(string) (mk20release.Outcome, error) {
			calls++
			return mk20release.Released, nil
		}, &wake))
		if err != nil {
			t.Fatal(err)
		}
		if calls != mk20ReleaseCandidateQuantum || result.scanned != mk20ReleaseCandidateQuantum || result.released != mk20ReleaseCandidateQuantum {
			t.Fatalf("calls=%d result=%+v", calls, result)
		}
		if wake != 1 {
			t.Fatalf("wake calls = %d, want 1", wake)
		}
	})

	t.Run("positive batch counts successful releases", func(t *testing.T) {
		wake := 0
		calls := 0
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{batch: 3}, defaultMK20ReleasePassDeps(ids, func(string) (mk20release.Outcome, error) {
			calls++
			if calls <= 2 {
				return mk20release.DoesNotFit, nil
			}
			return mk20release.Released, nil
		}, &wake))
		if err != nil {
			t.Fatal(err)
		}
		if calls != 5 || result.released != 3 || result.deferred != 2 {
			t.Fatalf("calls=%d result=%+v", calls, result)
		}
		if wake != 1 {
			t.Fatalf("wake calls = %d, want 1", wake)
		}
	})
}

func TestMK20ReleasePassCapacityAndPressure(t *testing.T) {
	t.Run("active cap avoids candidate scan", func(t *testing.T) {
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called while full")
			return "", nil
		}, &wake)
		candidateCalls := 0
		deps.active = func(context.Context) (int64, error) { return 200, nil }
		deps.candidates = func(context.Context, string, int) ([]string, error) {
			candidateCalls++
			return []string{"a"}, nil
		}
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{maxActive: 200}, deps)
		if err != nil {
			t.Fatal(err)
		}
		if candidateCalls != 0 || result.released != 0 || wake != 0 {
			t.Fatalf("candidateCalls=%d result=%+v wake=%d", candidateCalls, result, wake)
		}
	})

	t.Run("authoritative full outcome stops the page", func(t *testing.T) {
		wake := 0
		calls := 0
		deps := defaultMK20ReleasePassDeps([]string{"a", "b", "c"}, func(string) (mk20release.Outcome, error) {
			calls++
			return mk20release.AtCapacity, nil
		}, &wake)
		deps.active = func(context.Context) (int64, error) { return 9, nil }
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{maxActive: 10}, deps)
		if err != nil || calls != 1 || result.scanned != 1 || result.released != 0 || wake != 0 {
			t.Fatalf("calls=%d result=%+v wake=%d err=%v", calls, result, wake, err)
		}
	})

	t.Run("over cap also releases nothing", func(t *testing.T) {
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called while over cap")
			return "", nil
		}, &wake)
		deps.active = func(context.Context) (int64, error) { return 250, nil }
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{maxActive: 200}, deps)
		if err != nil || result.released != 0 || wake != 0 {
			t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
		}
	})

	t.Run("pressure clears on next pass", func(t *testing.T) {
		wake := 0
		checks := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			return mk20release.Released, nil
		}, &wake)
		deps.pressure = func(context.Context) (bool, error) {
			checks++
			return checks == 1, nil
		}
		policy := mk20ReleasePolicy{batch: 1}
		first, err := runMK20ReleasePass(context.Background(), "", policy, deps)
		if err != nil || first.released != 0 {
			t.Fatalf("first=%+v err=%v", first, err)
		}
		second, err := runMK20ReleasePass(context.Background(), "", policy, deps)
		if err != nil || second.released != 1 || checks != 2 || wake != 1 {
			t.Fatalf("second=%+v checks=%d wake=%d err=%v", second, checks, wake, err)
		}
	})

	t.Run("pressure error fails closed", func(t *testing.T) {
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
			t.Fatal("release called after pressure error")
			return "", nil
		}, &wake)
		deps.pressure = func(context.Context) (bool, error) { return false, errors.New("pressure query") }
		result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{maxActive: 10}, deps)
		if err == nil || result.released != 0 || wake != 0 {
			t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
		}
	})
}

func TestMK20ReleasePassSkipsPoisonOversizedAndDuplicates(t *testing.T) {
	ids := []string{"001", "002", "002", "003", "004"}
	wake := 0
	var called []string
	deps := defaultMK20ReleasePassDeps(ids, func(id string) (mk20release.Outcome, error) {
		called = append(called, id)
		switch id {
		case "001":
			return "", errors.New("malformed deal")
		case "002":
			return mk20release.TooLarge, nil
		case "003":
			return mk20release.DoesNotFit, nil
		default:
			return mk20release.Released, nil
		}
	}, &wake)
	result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{maxActive: 10}, deps)
	if err == nil {
		t.Fatal("poison candidate error was not reported")
	}
	wantCalled := []string{"001", "002", "003", "004"}
	if fmt.Sprint(called) != fmt.Sprint(wantCalled) {
		t.Fatalf("called = %v, want %v", called, wantCalled)
	}
	if result.released != 1 || result.tooLarge != 1 || result.deferred != 1 || result.scanned != 4 || wake != 1 {
		t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
	}
}

func TestMK20ReleasePassCursorReachesValidRowsBehindBadPages(t *testing.T) {
	ids := make([]string, 131)
	for i := range ids {
		ids[i] = fmt.Sprintf("%03d", i)
	}

	wake := 0
	cursor := ""
	validReached := false
	for pass := 0; pass < 3; pass++ {
		deps := defaultMK20ReleasePassDeps(ids, func(id string) (mk20release.Outcome, error) {
			switch {
			case id < "064":
				return "", errors.New("permanently malformed")
			case id < "130":
				return mk20release.TooLarge, nil
			default:
				validReached = true
				return mk20release.Released, nil
			}
		}, &wake)
		result, _ := runMK20ReleasePass(context.Background(), cursor, mk20ReleasePolicy{maxActive: 10}, deps)
		cursor = result.cursor
	}

	if !validReached {
		t.Fatal("valid row behind malformed and oversized pages was starved")
	}
	if wake != 1 {
		t.Fatalf("wake calls = %d, want 1", wake)
	}
}

func TestMK20ReleasePassWakeAfterLaterFailure(t *testing.T) {
	wake := 0
	deps := defaultMK20ReleasePassDeps([]string{"a", "b"}, func(id string) (mk20release.Outcome, error) {
		if id == "a" {
			return mk20release.Released, nil
		}
		return "", errors.New("later failure")
	}, &wake)
	result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{}, deps)
	if err == nil || result.released != 1 || wake != 1 {
		t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
	}

	wake = 0
	deps = defaultMK20ReleasePassDeps([]string{"a"}, func(string) (mk20release.Outcome, error) {
		return "", errors.New("failure")
	}, &wake)
	if _, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{}, deps); err == nil || wake != 0 {
		t.Fatalf("wake=%d err=%v", wake, err)
	}

	t.Run("committed work still wakes after cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		wake := 0
		deps := defaultMK20ReleasePassDeps([]string{"a", "b"}, func(string) (mk20release.Outcome, error) {
			cancel()
			return mk20release.Released, nil
		}, &wake)
		result, err := runMK20ReleasePass(ctx, "", mk20ReleasePolicy{}, deps)
		if !errors.Is(err, context.Canceled) || result.released != 1 || wake != 1 {
			t.Fatalf("result=%+v wake=%d err=%v", result, wake, err)
		}
	})
}

func TestMK20ReleasePassGateFailureStopsImmediately(t *testing.T) {
	wake := 0
	calls := 0
	deps := defaultMK20ReleasePassDeps([]string{"a", "b"}, func(string) (mk20release.Outcome, error) {
		calls++
		return "", fmt.Errorf("%w: missing singleton", mk20release.ErrGateUnavailable)
	}, &wake)
	result, err := runMK20ReleasePass(context.Background(), "", mk20ReleasePolicy{}, deps)
	if !errors.Is(err, mk20release.ErrGateUnavailable) || calls != 1 || result.scanned != 1 || wake != 0 {
		t.Fatalf("calls=%d result=%+v wake=%d err=%v", calls, result, wake, err)
	}
}

func TestMK20ReleasePassCursorWrapAndCancellation(t *testing.T) {
	wake := 0
	var afters []string
	deps := defaultMK20ReleasePassDeps(nil, func(string) (mk20release.Outcome, error) {
		return mk20release.Released, nil
	}, &wake)
	deps.candidates = func(_ context.Context, after string, _ int) ([]string, error) {
		afters = append(afters, after)
		if after == "z" {
			return nil, nil
		}
		return []string{"a"}, nil
	}
	result, err := runMK20ReleasePass(context.Background(), "z", mk20ReleasePolicy{}, deps)
	if err != nil || fmt.Sprint(afters) != fmt.Sprint([]string{"z", ""}) || result.cursor != "a" || result.released != 1 {
		t.Fatalf("afters=%v result=%+v err=%v", afters, result, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	wake = 0
	candidateCalls := 0
	deps.candidates = func(context.Context, string, int) ([]string, error) {
		candidateCalls++
		return nil, nil
	}
	if _, err := runMK20ReleasePass(ctx, "", mk20ReleasePolicy{}, deps); !errors.Is(err, context.Canceled) || candidateCalls != 0 || wake != 0 {
		t.Fatalf("candidateCalls=%d wake=%d err=%v", candidateCalls, wake, err)
	}
}

func TestMK20ReleaseModelDrains32108WithoutExceedingCap(t *testing.T) {
	const (
		backlog = 32108
		capRows = 200
	)
	waiting := make(map[string]struct{}, backlog)
	for i := range backlog {
		waiting[fmt.Sprintf("%05d", i)] = struct{}{}
	}
	active := make(map[string]struct{}, capRows)
	releaseCount := make(map[string]int, backlog)
	cursor := ""
	maxObserved := 0
	wakes := 0

	for pass := 0; len(waiting) > 0 && pass < 2000; pass++ {
		deps := mk20ReleasePassDeps{
			waiting:  func(context.Context) (bool, error) { return len(waiting) > 0, nil },
			pressure: func(context.Context) (bool, error) { return false, nil },
			active:   func(context.Context) (int64, error) { return int64(len(active)), nil },
			candidates: func(_ context.Context, after string, limit int) ([]string, error) {
				ids := make([]string, 0, len(waiting))
				for id := range waiting {
					if id > after {
						ids = append(ids, id)
					}
				}
				sort.Strings(ids)
				if len(ids) > limit {
					ids = ids[:limit]
				}
				return ids, nil
			},
			release: func(_ context.Context, id string, maxActive int64) (mk20release.Outcome, error) {
				if _, ok := waiting[id]; !ok {
					return mk20release.NoLongerWaiting, nil
				}
				if int64(len(active)) >= maxActive {
					return mk20release.AtCapacity, nil
				}
				delete(waiting, id)
				active[id] = struct{}{}
				releaseCount[id]++
				if len(active) > maxObserved {
					maxObserved = len(active)
				}
				return mk20release.Released, nil
			},
			wakeDealPoller: func() { wakes++ },
		}
		result, err := runMK20ReleasePass(context.Background(), cursor, mk20ReleasePolicy{maxActive: capRows}, deps)
		if err != nil {
			t.Fatal(err)
		}
		cursor = result.cursor

		// Model downstream completion returning slots. This is not a sealing
		// throughput test; it only exercises repeated release/refill behavior.
		completed := 0
		for id := range active {
			delete(active, id)
			completed++
			if completed == 32 {
				break
			}
		}
	}

	if len(waiting) != 0 {
		t.Fatalf("waiting rows remaining = %d", len(waiting))
	}
	if maxObserved > capRows {
		t.Fatalf("active rows reached %d, cap %d", maxObserved, capRows)
	}
	if len(releaseCount) != backlog {
		t.Fatalf("released IDs = %d, want %d", len(releaseCount), backlog)
	}
	for id, count := range releaseCount {
		if count != 1 {
			t.Fatalf("deal %s released %d times", id, count)
		}
	}
	if wakes == 0 {
		t.Fatal("no poller wakes recorded")
	}
}

func TestMK20PipelineRowCostAndScheduleAreUnchanged(t *testing.T) {
	start := abi.ChainEpoch(12345)
	deal := &mk20.Deal{
		Products: mk20.Products{DDOV1: &mk20.DDOV1{StartEpoch: &start, Duration: 5256000}},
		Data:     &mk20.DataSource{SourceHTTP: &mk20.DataSourceHTTP{}},
	}

	tests := []struct {
		name string
		set  func()
		want int64
		err  bool
	}{
		{name: "HTTP", set: func() { deal.Data = &mk20.DataSource{SourceHTTP: &mk20.DataSourceHTTP{}} }, want: 1},
		{name: "offline", set: func() { deal.Data = &mk20.DataSource{SourceOffline: &mk20.DataSourceOffline{}} }, want: 1},
		{name: "aggregate", set: func() {
			pieces := make([]mk20.DataSource, 7)
			for i := range pieces {
				pieces[i].SourceOffline = &mk20.DataSourceOffline{}
			}
			deal.Data = &mk20.DataSource{SourceAggregate: &mk20.DataSourceAggregate{Pieces: pieces}}
		}, want: 7},
		{name: "empty aggregate", set: func() { deal.Data = &mk20.DataSource{SourceAggregate: &mk20.DataSourceAggregate{}} }, err: true},
		{name: "aggregate subpiece missing source", set: func() {
			deal.Data = &mk20.DataSource{SourceAggregate: &mk20.DataSourceAggregate{Pieces: []mk20.DataSource{{}}}}
		}, err: true},
		{name: "aggregate subpiece multiple sources", set: func() {
			deal.Data = &mk20.DataSource{SourceAggregate: &mk20.DataSourceAggregate{Pieces: []mk20.DataSource{{
				SourceHTTP:    &mk20.DataSourceHTTP{},
				SourceOffline: &mk20.DataSourceOffline{},
			}}}}
		}, err: true},
		{name: "multiple top-level sources", set: func() {
			deal.Data = &mk20.DataSource{SourceHTTP: &mk20.DataSourceHTTP{}, SourceOffline: &mk20.DataSourceOffline{}}
		}, err: true},
		{name: "upload is separate", set: func() { deal.Data = &mk20.DataSource{SourceHttpPut: &mk20.DataSourceHttpPut{}} }, err: true},
		{name: "missing source", set: func() { deal.Data = &mk20.DataSource{} }, err: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.set()
			got, err := mk20PipelineRowCost(deal)
			if (err != nil) != tc.err || got != tc.want {
				t.Fatalf("rows=%d err=%v, want rows=%d err=%v", got, err, tc.want, tc.err)
			}
			if deal.Products.DDOV1.Duration != 5256000 || deal.Products.DDOV1.StartEpoch == nil || *deal.Products.DDOV1.StartEpoch != start {
				t.Fatalf("release planning changed DDO schedule: %+v", deal.Products.DDOV1)
			}
		})
	}

	if _, err := mk20PipelineRowCost(nil); err == nil {
		t.Fatal("nil deal accepted")
	}
	if _, err := mk20PipelineRowCost(&mk20.Deal{Data: &mk20.DataSource{SourceOffline: &mk20.DataSourceOffline{}}}); err == nil {
		t.Fatal("deal without DDO accepted")
	}
}
