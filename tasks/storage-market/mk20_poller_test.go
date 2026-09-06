package storage_market

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

func TestFullMK20PiecePassMarksBeforeLoading(t *testing.T) {
	ctx := context.Background()
	stored := []MK20PipelinePiece{
		{ID: "deal-1"},
		{ID: "deal-2"},
		{ID: "deal-3"},
	}
	var snapshot []MK20PipelinePiece
	var operations []string

	err := markThenLoadMK20Pieces(ctx, func(context.Context) error {
		operations = append(operations, "mark")
		for i := range stored {
			stored[i].Downloaded = true
		}
		return nil
	}, func(context.Context) error {
		operations = append(operations, "load")
		snapshot = append(snapshot, stored...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(operations, []string{"mark", "load"}) {
		t.Fatalf("operations = %v, want [mark load]", operations)
	}
	if len(snapshot) != len(stored) {
		t.Fatalf("loaded %d pieces, want %d", len(snapshot), len(stored))
	}
	for _, piece := range snapshot {
		if !piece.Downloaded {
			t.Fatalf("piece %s has stale downloaded=false", piece.ID)
		}
	}
}

func TestFullMK20PiecePassStopsWhenMarkingFails(t *testing.T) {
	markErr := errors.New("mark failed")
	loaded := false
	err := markThenLoadMK20Pieces(context.Background(), func(context.Context) error {
		return markErr
	}, func(context.Context) error {
		loaded = true
		return nil
	})
	if !errors.Is(err, markErr) {
		t.Fatalf("error = %v, want %v", err, markErr)
	}
	if loaded {
		t.Fatal("loaded a pipeline snapshot after downloaded marking failed")
	}
}

func TestSignalNextMK20ProcessesOnlyLoadedDealAndWakes(t *testing.T) {
	ctx := context.Background()
	requestedID := "requested-deal"
	stored := []MK20PipelinePiece{
		{ID: requestedID, PieceCID: "piece-1"},
		{ID: requestedID, PieceCID: "piece-2"},
		{ID: "other-deal", PieceCID: "piece-3"},
	}
	var processed []string
	var operations []string
	wakes := 0

	err := runSignalNextMK20(ctx, requestedID, func(_ context.Context, id string) ([]MK20PipelinePiece, error) {
		operations = append(operations, "load:"+id)
		var pieces []MK20PipelinePiece
		for _, piece := range stored {
			if piece.ID == id {
				pieces = append(pieces, piece)
			}
		}
		return pieces, nil
	}, func(_ context.Context, piece MK20PipelinePiece) error {
		operations = append(operations, "piece:"+piece.PieceCID)
		processed = append(processed, piece.ID)
		return nil
	}, func(piece MK20PipelinePiece, err error) {
		t.Fatalf("unexpected error for piece %s: %v", piece.PieceCID, err)
	}, func() {
		operations = append(operations, "wake")
		wakes++
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(processed, []string{requestedID, requestedID}) {
		t.Fatalf("processed deal IDs = %v", processed)
	}
	wantOperations := []string{"load:" + requestedID, "piece:piece-1", "piece:piece-2", "wake"}
	if !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("operations = %v, want %v", operations, wantOperations)
	}
	if wakes != 1 {
		t.Fatalf("wake calls = %d, want 1", wakes)
	}
}

func TestSignalNextMK20PieceErrorContinuesAndWakesOnce(t *testing.T) {
	pieceErr := errors.New("piece failed")
	pieces := []MK20PipelinePiece{{ID: "deal", PieceCID: "first"}, {ID: "deal", PieceCID: "second"}}
	var processed, failed []string
	wakes := 0

	err := runSignalNextMK20(context.Background(), "deal", func(context.Context, string) ([]MK20PipelinePiece, error) {
		return pieces, nil
	}, func(_ context.Context, piece MK20PipelinePiece) error {
		processed = append(processed, piece.PieceCID)
		if piece.PieceCID == "first" {
			return pieceErr
		}
		return nil
	}, func(piece MK20PipelinePiece, err error) {
		if !errors.Is(err, pieceErr) {
			t.Fatalf("piece error = %v, want %v", err, pieceErr)
		}
		failed = append(failed, piece.PieceCID)
	}, func() {
		wakes++
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(processed, []string{"first", "second"}) {
		t.Fatalf("processed = %v", processed)
	}
	if !reflect.DeepEqual(failed, []string{"first"}) {
		t.Fatalf("failed = %v", failed)
	}
	if wakes != 1 {
		t.Fatalf("wake calls = %d, want 1", wakes)
	}
}

func TestSignalNextMK20CancellationStopsWithoutWake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pieces := []MK20PipelinePiece{{ID: "deal", PieceCID: "first"}, {ID: "deal", PieceCID: "second"}}
	var processed []string
	wakes := 0

	err := runSignalNextMK20(ctx, "deal", func(context.Context, string) ([]MK20PipelinePiece, error) {
		return pieces, nil
	}, func(_ context.Context, piece MK20PipelinePiece) error {
		processed = append(processed, piece.PieceCID)
		cancel()
		return context.Canceled
	}, func(MK20PipelinePiece, error) {}, func() {
		wakes++
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if !reflect.DeepEqual(processed, []string{"first"}) {
		t.Fatalf("processed = %v, want [first]", processed)
	}
	if wakes != 0 {
		t.Fatalf("wake calls = %d, want 0", wakes)
	}
}

func TestSignalNextMK20LoadErrorDoesNotWake(t *testing.T) {
	loadErr := errors.New("load failed")
	wakes := 0
	err := runSignalNextMK20(context.Background(), "deal", func(context.Context, string) ([]MK20PipelinePiece, error) {
		return nil, loadErr
	}, func(context.Context, MK20PipelinePiece) error {
		t.Fatal("processor called after load error")
		return nil
	}, func(MK20PipelinePiece, error) {
		t.Fatal("error handler called after load error")
	}, func() {
		wakes++
	})
	if !errors.Is(err, loadErr) {
		t.Fatalf("error = %v, want %v", err, loadErr)
	}
	if wakes != 0 {
		t.Fatalf("wake calls = %d, want 0", wakes)
	}
}

func TestSignalNextMK20RepeatedCallbacksCoalesceWake(t *testing.T) {
	d := &CurioStorageDealMarket{
		wakePollReq:        make(chan struct{}),
		wakePollLoopCtx:    context.Background(),
		lastWakeDrivenPoll: time.Now().Add(time.Hour),
	}
	t.Cleanup(func() {
		d.wakeMu.Lock()
		defer d.wakeMu.Unlock()
		if d.wakePollTimer != nil {
			d.wakePollTimer.Stop()
			d.wakePollTimer = nil
		}
	})

	run := func() {
		err := runSignalNextMK20(context.Background(), "deal", func(context.Context, string) ([]MK20PipelinePiece, error) {
			return nil, nil
		}, func(context.Context, MK20PipelinePiece) error {
			return nil
		}, func(MK20PipelinePiece, error) {}, d.WakeDealPoller)
		if err != nil {
			t.Fatal(err)
		}
	}

	run()
	d.wakeMu.Lock()
	firstTimer := d.wakePollTimer
	d.wakeMu.Unlock()
	if firstTimer == nil {
		t.Fatal("first callback did not schedule a wake")
	}

	run()
	d.wakeMu.Lock()
	secondTimer := d.wakePollTimer
	d.wakeMu.Unlock()
	if secondTimer != firstTimer {
		t.Fatal("second callback scheduled a second wake")
	}
}

func TestWakeDealPollerShutdownDoesNotBlockDelivery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &CurioStorageDealMarket{
		wakePollReq:     make(chan struct{}),
		wakePollLoopCtx: ctx,
	}

	d.WakeDealPoller()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.wakeMu.Lock()
		pending := d.wakePollTimer != nil
		d.wakeMu.Unlock()
		if !pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("wake delivery remained blocked after shutdown")
}

func TestMK20GlobalStagesRemainInFullPollOrder(t *testing.T) {
	calls := methodCallsInFunction(t, "mk20.go", "processMK20Deals")
	want := []string{"processMK20DealPieces", "processMK20DealAggregation", "processMK20DealIngestion"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("global stage calls = %v, want %v", calls, want)
	}
}

func TestSignalNextMK20ProductionPathHasNoGlobalStages(t *testing.T) {
	calls := methodCallsInFunction(t, "storage_market.go", "signalNextMK20")
	for _, call := range calls {
		if call == "processMK20DealAggregation" || call == "processMK20DealIngestion" || call == "markMK20Downloaded" {
			t.Fatalf("signalNextMK20 directly calls global stage %s", call)
		}
	}
}

func TestPerPieceMK20ProcessingDoesNotMarkDownloadsGlobally(t *testing.T) {
	calls := methodCallsInFunction(t, "mk20.go", "processMk20Pieces")
	for _, call := range calls {
		if call == "markMK20Downloaded" {
			t.Fatal("processMk20Pieces calls global downloaded marking")
		}
	}
}

func methodCallsInFunction(t *testing.T, filename, function string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == function {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatalf("function %s not found in %s", function, filename)
	}

	wanted := map[string]struct{}{
		"processMK20DealPieces":      {},
		"processMK20DealAggregation": {},
		"processMK20DealIngestion":   {},
		"markMK20Downloaded":         {},
	}
	var calls []string
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, ok := wanted[sel.Sel.Name]; ok {
			calls = append(calls, sel.Sel.Name)
		}
		return true
	})
	return calls
}
