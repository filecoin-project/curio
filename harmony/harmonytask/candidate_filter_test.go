package harmonytask

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"
)

type candidateFilterStub struct {
	TaskInterface
	filter func(context.Context, []TaskID) ([]TaskID, error)
}

func (s candidateFilterStub) FilterCandidates(ctx context.Context, ids []TaskID) ([]TaskID, error) {
	return s.filter(ctx, ids)
}

func TestCandidateFilterBeforeSnapshotBound(t *testing.T) {
	for _, invalid := range []int{1, chokePoint + 1, 31257} {
		tasks := make([]task, invalid+2)
		for i := range tasks {
			tasks[i] = task{ID: TaskID(i + 1), PostedTime: time.Unix(int64(i), 0), Retries: i % 2}
		}
		original := append([]task(nil), tasks...)
		calls := 0
		observed := 0
		h := &taskTypeHandler{TaskInterface: candidateFilterStub{filter: func(ctx context.Context, ids []TaskID) ([]TaskID, error) {
			calls++
			observed = len(ids)
			if d, ok := ctx.Deadline(); !ok || time.Until(d) > candidateFilterTimeout {
				t.Fatal("unbounded filter")
			}
			return []TaskID{TaskID(invalid + 2), TaskID(invalid + 1)}, nil
		}}}
		for pass := 0; pass < 3; pass++ {
			got, err := filterPolledTasks(context.Background(), h, tasks)
			if err != nil || !reflect.DeepEqual(got, tasks[invalid:]) {
				t.Fatalf("later valid work lost: invalid=%d got=%v err=%v", invalid, got, err)
			}
		}
		if calls != 3 || observed != len(tasks) || !reflect.DeepEqual(tasks, original) {
			t.Fatal("unexpected calls or input mutation")
		}
	}
}

func TestCandidateFilterSnapshotBoundAndLegacyOrder(t *testing.T) {
	tasks := make([]task, chokePoint+3)
	for i := range tasks {
		tasks[i].ID = TaskID(i + 1)
	}
	for _, impl := range []TaskInterface{&stubAcceptTask{}, candidateFilterStub{filter: func(_ context.Context, ids []TaskID) ([]TaskID, error) { return ids, nil }}} {
		got, err := filterPolledTasks(context.Background(), &taskTypeHandler{TaskInterface: impl}, tasks)
		if err != nil || !reflect.DeepEqual(got, tasks[:chokePoint]) {
			t.Fatal("snapshot bound/order changed", err)
		}
	}
}

func TestCandidateFilterErrorCancellationAndEmpty(t *testing.T) {
	boom := errors.New("synthetic SQL unavailable")
	for _, mode := range []string{"sql-error", "cancelled", "deadline", "cancel-after-read", "empty", "unrelated-and-duplicate"} {
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
			calls := 0
			impl := candidateFilterStub{filter: func(ctx context.Context, ids []TaskID) ([]TaskID, error) {
				calls++
				if mode == "sql-error" {
					return []TaskID{2}, boom
				}
				if mode == "cancel-after-read" {
					cancel()
					return nil, nil
				}
				if mode == "empty" {
					return nil, nil
				}
				return []TaskID{2, 2, 900}, nil
			}}
			got, err := filterTaskCandidates(ctx, impl, []TaskID{1, 2})
			switch mode {
			case "sql-error":
				if !errors.Is(err, boom) || len(got) != 0 {
					t.Fatal(got, err)
				}
			case "cancelled", "deadline", "cancel-after-read":
				if err == nil || len(got) != 0 {
					t.Fatal("cancellation became valid empty snapshot", got, err)
				}
			case "empty":
				if err != nil || len(got) != 0 {
					t.Fatal(got, err)
				}
			default:
				if err != nil || !reflect.DeepEqual(got, []TaskID{2}) {
					t.Fatal(got, err)
				}
			}
			if (mode == "cancelled" || mode == "deadline") && calls != 0 {
				t.Fatal("queried after cancellation")
			}
		})
	}
}

func TestCandidateFilterProductionPollPath(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "scheduler.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	prematureCaps := 0
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "pollAllTaskTypesWithQuery" {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "chokePoint" {
					prematureCaps++
				}
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "filterPolledTasks" {
						calls++
					}
				}
				return true
			})
		}
	}
	if calls != 1 || prematureCaps != 0 {
		t.Fatal("real poll path does not use tested pre-cap filter")
	}
}

func TestCandidateRefusalLogIsRateLimited(t *testing.T) {
	h := &taskTypeHandler{}
	now := time.Unix(100, 0)
	if !h.shouldLogAcceptRefusal(now) {
		t.Fatal("missing first diagnostic")
	}
	for i := 0; i < 1000; i++ {
		if h.shouldLogAcceptRefusal(now.Add(time.Duration(i) * time.Millisecond)) {
			t.Fatal("unbounded refusal logs")
		}
	}
	if !h.shouldLogAcceptRefusal(now.Add(time.Minute)) {
		t.Fatal("diagnostic never resumes")
	}
}
