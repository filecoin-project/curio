package mk20release

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
)

// This model verifies orchestration and rollback. Its mutex is deliberately
// process-local and is not evidence for PostgreSQL/Yugabyte lock behavior;
// database integration tests must exercise the real singleton-row write.
type modelDB struct {
	mu      sync.Mutex
	waiting map[string]bool
	rows    map[string]int64
	other   int64
}

type modelTx struct {
	db          *modelDB
	waitingRows map[string]bool
	rows        map[string]int64
	other       int64
	locked      bool

	fail                  string
	calls                 map[string]int
	ops                   []string
	planRows              int64
	actualRows            int64
	prepareCalls          int
	activeOverride        []int64
	activeOverrideIndex   int
	gateUnavailable       bool
	waitingDeleteRowCount int
}

var errInjected = errors.New("injected failure")

func newModel() *modelDB {
	return &modelDB{
		waiting: map[string]bool{"deal": true},
		rows:    map[string]int64{},
	}
}

func attempt(db *modelDB, planRows, actualRows int64) *modelTx {
	return &modelTx{
		db:                    db,
		planRows:              planRows,
		actualRows:            actualRows,
		calls:                 map[string]int{},
		waitingDeleteRowCount: 1,
	}
}

func (m *modelTx) check(op string) error {
	m.calls[op]++
	m.ops = append(m.ops, op)
	if m.fail == op || m.fail == fmt.Sprintf("%s%d", op, m.calls[op]) {
		return errInjected
	}
	return nil
}

func (m *modelTx) lock() error {
	if err := m.check("lock"); err != nil {
		return err
	}
	if m.gateUnavailable {
		return fmt.Errorf("%w: model singleton row is absent", ErrGateUnavailable)
	}
	m.db.mu.Lock()
	m.locked = true
	m.waitingRows = cloneMap(m.db.waiting)
	m.rows = cloneMap(m.db.rows)
	m.other = m.db.other
	return nil
}

func (m *modelTx) waiting(id string) (bool, error) {
	if err := m.check("waiting"); err != nil {
		return false, err
	}
	return m.waitingRows[id], nil
}

func (m *modelTx) active() (int64, error) {
	if err := m.check("active"); err != nil {
		return 0, err
	}
	if m.activeOverrideIndex < len(m.activeOverride) {
		result := m.activeOverride[m.activeOverrideIndex]
		m.activeOverrideIndex++
		return result, nil
	}
	n := m.other
	for _, rows := range m.rows {
		n += rows
	}
	return n, nil
}

func (m *modelTx) dealRows(id string) (int64, error) {
	if err := m.check("dealRows"); err != nil {
		return 0, err
	}
	return m.rows[id], nil
}

func (m *modelTx) removeWaiting(id string) (int, error) {
	if err := m.check("delete"); err != nil {
		return 0, err
	}
	delete(m.waitingRows, id)
	return m.waitingDeleteRowCount, nil
}

func (m *modelTx) prepare(id string) (Plan, error) {
	m.prepareCalls++
	if err := m.check("prepare"); err != nil {
		return Plan{}, err
	}
	return Plan{Rows: m.planRows, Insert: func() error {
		// Write first so an injected insert error verifies outer rollback.
		m.rows[id] += m.actualRows
		return m.check("insert")
	}}, nil
}

func (m *modelTx) finish(commit bool) {
	if !m.locked {
		return
	}
	if commit {
		m.db.waiting = m.waitingRows
		m.db.rows = m.rows
	}
	m.db.mu.Unlock()
	m.locked = false
}

func (m *modelTx) run(id string, maxActive int64) (Outcome, error) {
	outcome, err := stage(m, id, maxActive, func() (Plan, error) {
		return m.prepare(id)
	})
	m.finish(err == nil && outcome == Released)
	return outcome, err
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func TestStageCapacityModes(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		active, cap, rows                 int64
		want                              Outcome
		wantActiveCalls, wantPrepareCalls int
	}{
		{name: "uncapped_still_gated", active: math.MaxInt64 - 1, cap: 0, rows: 2, want: Released, wantPrepareCalls: 1},
		{name: "single", cap: 200, rows: 1, want: Released, wantActiveCalls: 2, wantPrepareCalls: 1},
		{name: "last_slot", active: 199, cap: 200, rows: 1, want: Released, wantActiveCalls: 2, wantPrepareCalls: 1},
		{name: "at_cap", active: 200, cap: 200, rows: 1, want: AtCapacity, wantActiveCalls: 1},
		{name: "existing_over_cap", active: 250, cap: 200, rows: 1, want: AtCapacity, wantActiveCalls: 1},
		{name: "aggregate_fits", active: 195, cap: 200, rows: 5, want: Released, wantActiveCalls: 2, wantPrepareCalls: 1},
		{name: "aggregate_does_not_fit", active: 196, cap: 200, rows: 5, want: DoesNotFit, wantActiveCalls: 1, wantPrepareCalls: 1},
		{name: "aggregate_too_large", cap: 200, rows: 201, want: TooLarge, wantActiveCalls: 1, wantPrepareCalls: 1},
		{name: "subtraction_avoids_overflow", active: math.MaxInt64 - 1, cap: math.MaxInt64, rows: 2, want: DoesNotFit, wantActiveCalls: 1, wantPrepareCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newModel()
			db.other = tc.active
			tx := attempt(db, tc.rows, tc.rows)

			outcome, err := tx.run("deal", tc.cap)
			if err != nil {
				t.Fatal(err)
			}
			if outcome != tc.want {
				t.Fatalf("outcome=%q, want %q", outcome, tc.want)
			}
			if tx.calls["lock"] != 1 {
				t.Fatalf("gate writes=%d, want 1", tx.calls["lock"])
			}
			if tx.calls["active"] != tc.wantActiveCalls {
				t.Fatalf("active calls=%d, want %d", tx.calls["active"], tc.wantActiveCalls)
			}
			if tx.prepareCalls != tc.wantPrepareCalls {
				t.Fatalf("prepare calls=%d, want %d", tx.prepareCalls, tc.wantPrepareCalls)
			}
			if outcome == Released {
				if db.waiting["deal"] {
					t.Fatal("released deal remained waiting")
				}
				if db.rows["deal"] != tc.rows {
					t.Fatalf("pipeline rows=%d, want %d", db.rows["deal"], tc.rows)
				}
			} else {
				if !db.waiting["deal"] || len(db.rows) != 0 {
					t.Fatal("deferred deal changed durable state")
				}
			}
		})
	}
}

func TestStageAuthoritativeChecksFollowGateWrite(t *testing.T) {
	tx := attempt(newModel(), 1, 1)
	if outcome, err := tx.run("deal", 200); err != nil || outcome != Released {
		t.Fatal(outcome, err)
	}
	want := []string{"lock", "waiting", "dealRows", "active", "prepare", "insert", "dealRows", "active", "delete"}
	if !slices.Equal(tx.ops, want) {
		t.Fatalf("operation order=%v, want %v", tx.ops, want)
	}
}

func TestStageFailuresRollback(t *testing.T) {
	for _, op := range []string{
		"lock", "waiting", "dealRows1", "active1", "prepare", "insert",
		"dealRows2", "active2", "delete",
	} {
		t.Run(op, func(t *testing.T) {
			db := newModel()
			tx := attempt(db, 1, 1)
			tx.fail = op

			outcome, err := tx.run("deal", 200)
			if err == nil || outcome == Released {
				t.Fatalf("failure accepted: outcome=%q err=%v", outcome, err)
			}
			if !errors.Is(err, errInjected) {
				t.Fatalf("wrapped error lost injected cause: %v", err)
			}
			if !db.waiting["deal"] || len(db.rows) != 0 {
				t.Fatal("partial transaction escaped rollback")
			}
		})
	}

	t.Run("waiting delete affected zero rows", func(t *testing.T) {
		db := newModel()
		tx := attempt(db, 1, 1)
		tx.waitingDeleteRowCount = 0
		outcome, err := tx.run("deal", 200)
		if err == nil || outcome == Released {
			t.Fatalf("bad delete accepted: outcome=%q err=%v", outcome, err)
		}
		if !db.waiting["deal"] || len(db.rows) != 0 {
			t.Fatal("delete-count failure escaped rollback")
		}
	})

	t.Run("post-insert cap invariant", func(t *testing.T) {
		db := newModel()
		tx := attempt(db, 1, 1)
		tx.activeOverride = []int64{0, 201}
		outcome, err := tx.run("deal", 200)
		if err == nil || outcome == Released {
			t.Fatalf("cap violation accepted: outcome=%q err=%v", outcome, err)
		}
		if !db.waiting["deal"] || len(db.rows) != 0 {
			t.Fatal("cap violation escaped rollback")
		}
	})
}

func TestStageRequiresExactInsertedRowCount(t *testing.T) {
	for _, actual := range []int64{0, 2, 201} {
		t.Run(fmt.Sprint(actual), func(t *testing.T) {
			db := newModel()
			outcome, err := attempt(db, 1, actual).run("deal", 200)
			if err == nil || outcome == Released {
				t.Fatalf("row mismatch accepted: outcome=%q err=%v", outcome, err)
			}
			if !db.waiting["deal"] || len(db.rows) != 0 {
				t.Fatal("row mismatch escaped rollback")
			}
		})
	}
}

func TestStageAlreadyReleasedAndInconsistentWaiting(t *testing.T) {
	db := newModel()
	if outcome, err := attempt(db, 1, 1).run("deal", 200); err != nil || outcome != Released {
		t.Fatal(outcome, err)
	}

	tx := attempt(db, 1, 1)
	if outcome, err := tx.run("deal", 200); err != nil || outcome != NoLongerWaiting {
		t.Fatalf("already released outcome=%q err=%v", outcome, err)
	}
	if tx.prepareCalls != 0 {
		t.Fatal("already released deal was prepared")
	}

	// Waiting+pipeline is checked before capacity, so an already-full cap
	// cannot conceal corrupt queue state.
	db.waiting["deal"] = true
	db.other = 200
	tx = attempt(db, 1, 1)
	if outcome, err := tx.run("deal", 200); err == nil || outcome == Released {
		t.Fatalf("inconsistent state accepted: outcome=%q err=%v", outcome, err)
	}
	if tx.calls["active"] != 0 || tx.prepareCalls != 0 {
		t.Fatal("inconsistent state reached capacity or prepare checks")
	}
	if db.rows["deal"] != 1 || !db.waiting["deal"] {
		t.Fatal("inconsistent state was modified")
	}
}

func TestStageFailsClosedWhenGateUnavailable(t *testing.T) {
	tx := attempt(newModel(), 1, 1)
	tx.gateUnavailable = true
	outcome, err := tx.run("deal", 0)
	if err == nil || outcome != "" || !errors.Is(err, ErrGateUnavailable) {
		t.Fatalf("gate failure outcome=%q err=%v", outcome, err)
	}
	if tx.calls["waiting"] != 0 || tx.prepareCalls != 0 {
		t.Fatal("release continued without the gate")
	}
}

func TestStageRejectsInvalidInputBeforeGate(t *testing.T) {
	for _, maxActive := range []int64{-1, math.MinInt64} {
		tx := attempt(newModel(), 1, 1)
		if _, err := tx.run("deal", maxActive); err == nil || tx.calls["lock"] != 0 {
			t.Fatalf("invalid cap %d touched the gate", maxActive)
		}
	}

	for _, rows := range []int64{0, -1} {
		tx := attempt(newModel(), rows, rows)
		if _, err := tx.run("deal", 200); err == nil {
			t.Fatalf("invalid row cost %d accepted", rows)
		}
	}

	tx := attempt(newModel(), 1, 1)
	if _, err := tx.run("", 200); err == nil || tx.calls["lock"] != 0 {
		t.Fatal("empty deal ID touched the gate")
	}
	if _, err := stage(nil, "deal", 200, func() (Plan, error) { return Plan{}, nil }); err == nil {
		t.Fatal("nil transaction accepted")
	}
	if _, err := stage(tx, "deal", 200, nil); err == nil {
		t.Fatal("nil prepare callback accepted")
	}
}

func TestReleaseUsesFinalRetryOutcome(t *testing.T) {
	db := newModel()
	var attempts int
	begin := func(callback func(transaction) (bool, error)) (bool, error) {
		attempts++
		first := attempt(db, 1, 1)
		commit, err := callback(first)
		if err != nil || !commit {
			first.finish(false)
			return false, fmt.Errorf("first callback: commit=%t err=%w", commit, err)
		}
		// Model a serialization failure: the first attempt did not commit and
		// another writer removed the waiting row before our fresh retry.
		first.finish(false)
		db.waiting["deal"] = false

		attempts++
		second := attempt(db, 1, 1)
		commit, err = callback(second)
		second.finish(commit && err == nil)
		return commit, err
	}

	prepareCalls := 0
	outcome, err := release("deal", 200, begin, func(tx transaction) (Plan, error) {
		prepareCalls++
		model := tx.(*modelTx)
		return model.prepare("deal")
	})
	if err != nil || outcome != NoLongerWaiting {
		t.Fatalf("final retry outcome=%q err=%v", outcome, err)
	}
	if attempts != 2 || prepareCalls != 1 {
		t.Fatalf("attempts=%d prepareCalls=%d", attempts, prepareCalls)
	}
	if len(db.rows) != 0 {
		t.Fatal("uncommitted first-attempt rows escaped")
	}
}

func TestReleaseReportsReleasedOnlyAfterCommit(t *testing.T) {
	t.Run("provisional release not committed", func(t *testing.T) {
		db := newModel()
		outcome, err := release("deal", 200, func(callback func(transaction) (bool, error)) (bool, error) {
			tx := attempt(db, 1, 1)
			commit, callbackErr := callback(tx)
			if callbackErr != nil || !commit {
				t.Fatalf("callback commit=%t err=%v", commit, callbackErr)
			}
			tx.finish(false)
			return false, nil
		}, func(tx transaction) (Plan, error) {
			return tx.(*modelTx).prepare("deal")
		})
		if err == nil || outcome == Released {
			t.Fatalf("uncommitted release reported: outcome=%q err=%v", outcome, err)
		}
		if !db.waiting["deal"] || len(db.rows) != 0 {
			t.Fatal("uncommitted release changed durable state")
		}
	})

	t.Run("commit without released outcome", func(t *testing.T) {
		db := newModel()
		db.other = 200
		outcome, err := release("deal", 200, func(callback func(transaction) (bool, error)) (bool, error) {
			tx := attempt(db, 1, 1)
			_, callbackErr := callback(tx)
			tx.finish(false)
			return true, callbackErr
		}, func(tx transaction) (Plan, error) {
			return tx.(*modelTx).prepare("deal")
		})
		if err == nil || outcome == Released {
			t.Fatalf("non-release commit accepted: outcome=%q err=%v", outcome, err)
		}
	})
}

func TestModelConcurrentReleasesRespectCap(t *testing.T) {
	db := newModel()
	delete(db.waiting, "deal")
	const (
		candidates = 128
		capRows    = 17
	)
	for n := 0; n < candidates; n++ {
		db.waiting[fmt.Sprint(n)] = true
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for n := 0; n < candidates; n++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			if _, err := attempt(db, 1, 1).run(id, capRows); err != nil {
				t.Errorf("release %s: %v", id, err)
			}
		}(fmt.Sprint(n))
	}
	close(start)
	wg.Wait()

	if len(db.rows) != capRows || len(db.waiting) != candidates-capRows {
		t.Fatalf("pipeline=%d waiting=%d", len(db.rows), len(db.waiting))
	}
}
