package harmonydb

// tests for BTFP mechanism - may be sensitive to Go version/inlining

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/samber/lo"
)

func TestBTFP_NestedTransaction(t *testing.T) {
	db := &DB{}
	ctx := context.Background()

	var innerErr error
	var reached bool

	func() {
		defer func() { recover() }()
		db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			reached = true
			_, innerErr = db.BeginTransaction(ctx, func(tx2 *Tx) (bool, error) {
				return false, nil
			})
			return false, innerErr
		})
	}()

	if !reached {
		t.Skip("need real DB")
	}
	if innerErr != errTx {
		t.Errorf("got %v, want %v", innerErr, errTx)
	}
}

func TestBTFP_ExecInsideTransaction(t *testing.T) {
	db := &DB{}
	ctx := context.Background()

	var execErr error
	var reached bool

	func() {
		defer func() { recover() }()
		db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			reached = true
			_, execErr = db.Exec(ctx, "SELECT 1")
			return false, execErr
		})
	}()

	if !reached {
		t.Skip("need real DB")
	}
	if execErr != errTx {
		t.Errorf("got %v, want %v", execErr, errTx)
	}
}

func TestBTFP_StoredValue(t *testing.T) {
	db := &DB{}
	ctx := context.Background()

	func() {
		defer func() { recover() }()
		db.BeginTransaction(ctx, func(tx *Tx) (bool, error) { return false, nil })
	}()

	btfp := db.BTFP.Load()
	if btfp == 0 {
		t.Fatal("BTFP not set")
	}

	fn := runtime.FuncForPC(uintptr(btfp))
	name := fn.Name()

	if !strings.Contains(name, "BeginTransaction") || strings.Contains(name, ".func") {
		t.Errorf("BTFP points to %q, want BeginTransaction", name)
	}
}

func TestBTFP_FoundInCallStack(t *testing.T) {
	db := &DB{}
	ctx := context.Background()

	func() {
		defer func() { recover() }()
		db.BeginTransaction(ctx, func(tx *Tx) (bool, error) { return false, nil })
	}()

	btfp := db.BTFP.Load()
	if btfp == 0 {
		t.Fatal("BTFP not set")
	}

	var found bool
	func() {
		defer func() { recover() }()
		db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
			var pcs [20]uintptr
			n := runtime.Callers(1, pcs[:])
			found = lo.Contains(pcs[:n], uintptr(btfp))
			return false, nil
		})
	}()

	if !found {
		t.Error("BTFP not found in call stack")
	}
}
