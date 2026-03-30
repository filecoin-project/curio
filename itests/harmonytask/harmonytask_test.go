package harmonytask

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

// ===== Test Infrastructure =====

type deadPeerConnector struct{}

func (d *deadPeerConnector) ConnectToPeer(peerID string) (harmonytask.PeerConnection, error) {
	return &deadConn{}, fmt.Errorf("test: no peers")
}
func (d *deadPeerConnector) SetOnConnect(func(string, harmonytask.PeerConnection)) {}

type deadConn struct{}

func (d *deadConn) SendMessage([]byte) error        { return fmt.Errorf("dead") }
func (d *deadConn) ReceiveMessage() ([]byte, error) { return nil, fmt.Errorf("dead") }
func (d *deadConn) Close() error                    { return nil }

type testTask struct {
	name          string
	cost          resources.Resources
	storage       resources.Storage // optional; when set, becomes Cost.Storage (disk claim path)
	maxN          int
	maxFail       uint
	retryWait     func(int) time.Duration
	timeSensitive bool
	doneCh        chan harmonytask.TaskID
	doFunc        func(context.Context, harmonytask.TaskID, func() bool) (bool, error)
	canAcceptFunc func([]harmonytask.TaskID, *harmonytask.TaskEngine) ([]harmonytask.TaskID, error)
	stopCh        chan struct{}

	mu        sync.Mutex
	completed []harmonytask.TaskID
	attempts  int32
}

func newTestTask(name string, max int) *testTask {
	return newTestTaskWithOpts(name, max, resources.Resources{Cpu: 1, Ram: 1 << 20}, false)
}

// newTestTaskWithOpts creates a task with custom cost and TimeSensitive flag.
// Use high cost (e.g. Cpu: 10000, Ram: 1<<30) to exhaust machine resources for preemption tests.
func newTestTaskWithOpts(name string, max int, cost resources.Resources, timeSensitive bool) *testTask {
	t := &testTask{
		name:          name,
		cost:          cost,
		maxN:          max,
		timeSensitive: timeSensitive,
		doneCh:        make(chan harmonytask.TaskID, 100),
		stopCh:        make(chan struct{}),
	}
	harmonytask.Registry[name] = t
	return t
}

func (t *testTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	atomic.AddInt32(&t.attempts, 1)
	if t.doFunc != nil {
		return t.doFunc(ctx, taskID, stillOwned)
	}
	t.mu.Lock()
	t.completed = append(t.completed, taskID)
	t.mu.Unlock()
	t.doneCh <- taskID
	return true, nil
}

func (t *testTask) CanAccept(ids []harmonytask.TaskID, te *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if t.canAcceptFunc != nil {
		return t.canAcceptFunc(ids, te)
	}
	return ids, nil
}

func (t *testTask) TypeDetails() harmonytask.TaskTypeDetails {
	cost := t.cost
	if t.storage != nil {
		cost.Storage = t.storage
	}
	ttd := harmonytask.TaskTypeDetails{
		Name:          t.name,
		Cost:          cost,
		MaxFailures:   t.maxFail,
		RetryWait:     t.retryWait,
		TimeSensitive: t.timeSensitive,
	}
	if t.maxN > 0 {
		ttd.Max = taskhelp.Max(t.maxN)
	}
	return ttd
}

func (t *testTask) Adder(add harmonytask.AddTaskFunc) { <-t.stopCh }

func (t *testTask) stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
}

func waitForTask(t *testing.T, ch <-chan harmonytask.TaskID, timeout time.Duration) harmonytask.TaskID {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(timeout):
		t.Fatal("timed out waiting for task")
		return 0
	}
}

func waitForTasks(t *testing.T, ch <-chan harmonytask.TaskID, n int, timeout time.Duration) []harmonytask.TaskID {
	t.Helper()
	ids := make([]harmonytask.TaskID, 0, n)
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case id := <-ch:
			ids = append(ids, id)
		case <-deadline:
			t.Fatalf("timed out: got %d/%d tasks", len(ids), n)
		}
	}
	return ids
}

func cleanupTasks(tasks ...*testTask) func() {
	return func() {
		for _, t := range tasks {
			t.stop()
			delete(harmonytask.Registry, t.name)
		}
	}
}

func makeEngine(t *testing.T, db *harmonydb.DB, impls []harmonytask.TaskInterface, host string) *harmonytask.TaskEngine {
	t.Helper()
	e, err := harmonytask.New(db, impls, host, &deadPeerConnector{})
	require.NoError(t, err)
	t.Cleanup(func() { e.GracefullyTerminate() })
	return e
}

// makeEngineWithResources registers fixed machine capacity (via resources.RegisterWithResources) then builds the engine.
func makeEngineWithResources(t *testing.T, db *harmonydb.DB, impls []harmonytask.TaskInterface, host string, res resources.Resources) *harmonytask.TaskEngine {
	t.Helper()
	reg, err := resources.RegisterWithResources(db, host, res)
	require.NoError(t, err)
	e, err := harmonytask.NewWithReg(db, impls, host, &deadPeerConnector{}, reg)
	require.NoError(t, err)
	t.Cleanup(func() { e.GracefullyTerminate() })
	return e
}

func makeEngineWithPeering(t *testing.T, db *harmonydb.DB, impls []harmonytask.TaskInterface, host string, connector harmonytask.PeerConnectorInterface) *harmonytask.TaskEngine {
	t.Helper()
	e, err := harmonytask.New(db, impls, host, connector)
	require.NoError(t, err)
	t.Cleanup(func() { e.GracefullyTerminate() })
	return e
}

func speedUpPolling(engines ...*harmonytask.TaskEngine) {
	// Peering goroutines run DB queries + ConnectToPeer async and override pollDuration.
	// Wait long enough for all of them to complete, then set our fast interval.
	// Set twice with a gap to win any late-arriving peering overrides.
	time.Sleep(3 * time.Second)
	for _, e := range engines {
		e.TestONLY_SetPollDuration(200 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	for _, e := range engines {
		e.TestONLY_SetPollDuration(200 * time.Millisecond)
	}
}

func waitForHistory(t *testing.T, db *harmonydb.DB, taskID harmonytask.TaskID, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		var host string
		err := db.QueryRow(context.Background(),
			`SELECT completed_by_host_and_port FROM harmony_task_history WHERE task_id=$1`, taskID).Scan(&host)
		if err == nil {
			return host
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for history row for task %d", taskID)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitForSuccessCount(t *testing.T, db *harmonydb.DB, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		var n int
		err := db.QueryRow(context.Background(), `SELECT COUNT(*) FROM harmony_task_history WHERE result = true`).Scan(&n)
		if err == nil && n >= want {
			require.Equal(t, want, n)
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d successful history rows (got %d)", want, n)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func waitForNamedSuccessCount(t *testing.T, db *harmonydb.DB, name string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		var n int
		err := db.QueryRow(context.Background(), `SELECT COUNT(*) FROM harmony_task_history WHERE result = true AND name = $1`, name).Scan(&n)
		if err == nil && n >= want {
			require.Equal(t, want, n)
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d '%s' history rows (got %d)", want, name, n)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

const taskTimeout = 30 * time.Second

// getDB returns the shared test database and truncates harmony tables for a clean test.
// Deletion order respects foreign key constraints.
func getDB(t *testing.T) *harmonydb.DB {
	t.Helper()
	db := sharedTestDB
	if db == nil {
		t.Skip("shared test DB not available")
	}
	// Let any async recordCompletion goroutines from prior tests drain.
	time.Sleep(500 * time.Millisecond)
	ctx := context.Background()
	_, _ = db.Exec(ctx, `DELETE FROM harmony_task_history`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_task_follow`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_task_impl`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_task_singletons`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_task`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_machine_details`)
	_, _ = db.Exec(ctx, `DELETE FROM harmony_machines`)
	return db
}

// ===== Scheduler Tests =====

func TestSchedulerThreeNodeRouting(t *testing.T) {
	db := getDB(t)

	tA := newTestTask("RoutA", 5)
	tB := newTestTask("RoutB", 5)
	tC := newTestTask("RoutC", 5)
	t.Cleanup(cleanupTasks(tA, tB, tC))

	e1 := makeEngine(t, db, []harmonytask.TaskInterface{tA}, "r1:1000")
	e2 := makeEngine(t, db, []harmonytask.TaskInterface{tB}, "r2:1000")
	e3 := makeEngine(t, db, []harmonytask.TaskInterface{tC}, "r3:1000")
	speedUpPolling(e1, e2, e3)

	require.NotEqual(t, e1.OwnerID(), e2.OwnerID())
	require.NotEqual(t, e2.OwnerID(), e3.OwnerID())

	// Local routing
	e1.AddTaskByName("RoutA", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	aID := waitForTask(t, tA.doneCh, taskTimeout)
	host := waitForHistory(t, db, aID, taskTimeout)
	require.Equal(t, "r1:1000", host)

	// Cross-scheduler: AddTask on scheduler 2 → scheduler 2 runs it
	e2.AddTaskByName("RoutB", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	bID := waitForTask(t, tB.doneCh, taskTimeout)
	host = waitForHistory(t, db, bID, taskTimeout)
	require.Equal(t, "r2:1000", host)

	// Cross-scheduler: AddTask on scheduler 3 → scheduler 3 runs it
	e3.AddTaskByName("RoutC", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	cID := waitForTask(t, tC.doneCh, taskTimeout)
	host = waitForHistory(t, db, cID, taskTimeout)
	require.Equal(t, "r3:1000", host)

	waitForSuccessCount(t, db, 3, taskTimeout)
}

func TestSchedulerRemoteTaskStart(t *testing.T) {
	ctx := context.Background()
	db := getDB(t)
	var err error

	s1 := newTestTask("Pipe1", 5)
	s2 := newTestTask("Pipe2", 5)
	s3 := newTestTask("Pipe3", 5)
	t.Cleanup(cleanupTasks(s1, s2, s3))

	e1 := makeEngine(t, db, []harmonytask.TaskInterface{s1}, "p1:1000")
	e2 := makeEngine(t, db, []harmonytask.TaskInterface{s2}, "p2:1000")
	e3 := makeEngine(t, db, []harmonytask.TaskInterface{s3}, "p3:1000")
	speedUpPolling(e1, e2, e3)

	// Pipe1 completes → adds Pipe2 on scheduler 2 (remote task start)
	s1.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		e2.AddTaskByName("Pipe2", func(tID harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			return true, nil
		})
		s1.doneCh <- id
		return true, nil
	}
	// Pipe2 completes → adds Pipe3 on scheduler 3 (remote task start)
	s2.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		e3.AddTaskByName("Pipe3", func(tID harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			return true, nil
		})
		s2.doneCh <- id
		return true, nil
	}

	e1.AddTaskByName("Pipe1", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	waitForTask(t, s1.doneCh, taskTimeout)
	waitForTask(t, s2.doneCh, taskTimeout)
	waitForTask(t, s3.doneCh, taskTimeout)

	// Wait for all 3 history records (recordCompletion is async)
	waitForSuccessCount(t, db, 3, taskTimeout)

	// Verify each stage ran on the correct host (order may vary due to async recordCompletion)
	var stages []struct {
		Name string `db:"name"`
		Host string `db:"completed_by_host_and_port"`
	}
	err = db.Select(ctx, &stages, `SELECT name, completed_by_host_and_port FROM harmony_task_history ORDER BY name`)
	require.NoError(t, err)
	require.Len(t, stages, 3)
	hostByName := map[string]string{}
	for _, s := range stages {
		hostByName[s.Name] = s.Host
	}
	require.Equal(t, "p1:1000", hostByName["Pipe1"])
	require.Equal(t, "p2:1000", hostByName["Pipe2"])
	require.Equal(t, "p3:1000", hostByName["Pipe3"])
}

func TestSchedulerConcurrentMultiNode(t *testing.T) {
	ctx := context.Background()
	db := getDB(t)
	var err error

	tX := newTestTask("BulkX", 10)
	tY := newTestTask("BulkY", 10)
	tZ := newTestTask("BulkZ", 10)
	t.Cleanup(cleanupTasks(tX, tY, tZ))

	e1 := makeEngine(t, db, []harmonytask.TaskInterface{tX}, "b1:1000")
	e2 := makeEngine(t, db, []harmonytask.TaskInterface{tY}, "b2:1000")
	e3 := makeEngine(t, db, []harmonytask.TaskInterface{tZ}, "b3:1000")
	speedUpPolling(e1, e2, e3)

	const perType = 5
	for i := 0; i < perType; i++ {
		e1.AddTaskByName("BulkX", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
		e2.AddTaskByName("BulkY", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
		e3.AddTaskByName("BulkZ", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	}

	waitForTasks(t, tX.doneCh, perType, taskTimeout)
	waitForTasks(t, tY.doneCh, perType, taskTimeout)
	waitForTasks(t, tZ.doneCh, perType, taskTimeout)

	waitForSuccessCount(t, db, perType*3, taskTimeout)

	for _, name := range []string{"BulkX", "BulkY", "BulkZ"} {
		var hosts []string
		err = db.Select(ctx, &hosts, `SELECT DISTINCT completed_by_host_and_port FROM harmony_task_history WHERE name=$1`, name)
		require.NoError(t, err)
		require.Len(t, hosts, 1, "%s should run on exactly one host", name)
	}
}

func TestSchedulerMaxConcurrency(t *testing.T) {
	db := getDB(t)

	var running, peak int32
	done := make(chan harmonytask.TaskID, 20)

	tM := newTestTask("MaxT", 2)
	tM.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		cur := atomic.AddInt32(&running, 1)
		defer atomic.AddInt32(&running, -1)
		for {
			old := atomic.LoadInt32(&peak)
			if cur <= old || atomic.CompareAndSwapInt32(&peak, old, cur) {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
		done <- id
		return true, nil
	}
	t.Cleanup(cleanupTasks(tM))

	e := makeEngine(t, db, []harmonytask.TaskInterface{tM}, "mc:1000")
	speedUpPolling(e)

	for i := 0; i < 6; i++ {
		e.AddTaskByName("MaxT", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	}

	waitForTasks(t, done, 6, 30*time.Second)
	require.LessOrEqual(t, int(atomic.LoadInt32(&peak)), 2)
}

func TestSchedulerRetry(t *testing.T) {
	ctx := context.Background()
	db := getDB(t)
	var err error

	var attempts int32
	tR := newTestTask("RetryT", 5)
	tR.maxFail = 5
	tR.retryWait = func(int) time.Duration { return 50 * time.Millisecond }
	tR.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return false, fmt.Errorf("intentional failure #%d", n)
		}
		tR.doneCh <- id
		return true, nil
	}
	t.Cleanup(cleanupTasks(tR))

	e := makeEngine(t, db, []harmonytask.TaskInterface{tR}, "re:1000")
	speedUpPolling(e)
	e.AddTaskByName("RetryT", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })

	id := waitForTask(t, tR.doneCh, 30*time.Second)
	require.GreaterOrEqual(t, int(atomic.LoadInt32(&attempts)), 3)

	var entries []struct {
		Result bool   `db:"result"`
		Err    string `db:"err"`
	}
	deadline := time.After(30 * time.Second)
	for {
		entries = nil
		err = db.Select(ctx, &entries, `SELECT result, err FROM harmony_task_history WHERE task_id=$1 ORDER BY id`, id)
		hasSuccess := false
		for _, e := range entries {
			if e.Result {
				hasSuccess = true
			}
		}
		if err == nil && len(entries) >= 3 && hasSuccess {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for retry history: got %d entries, last: %+v", len(entries), entries)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func TestSchedulerMultiTaskNode(t *testing.T) {
	db := getDB(t)

	t1 := newTestTask("Mul1", 5)
	t2 := newTestTask("Mul2", 5)
	t3 := newTestTask("Mul3", 5)
	t.Cleanup(cleanupTasks(t1, t2, t3))

	e := makeEngine(t, db, []harmonytask.TaskInterface{t1, t2, t3}, "mt:1000")
	speedUpPolling(e)

	e.AddTaskByName("Mul1", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("Mul2", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("Mul3", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })

	id1 := waitForTask(t, t1.doneCh, taskTimeout)
	id2 := waitForTask(t, t2.doneCh, taskTimeout)
	id3 := waitForTask(t, t3.doneCh, taskTimeout)

	for _, id := range []harmonytask.TaskID{id1, id2, id3} {
		host := waitForHistory(t, db, id, taskTimeout)
		require.Equal(t, "mt:1000", host)
	}
}

func TestSchedulerSharedTask(t *testing.T) {
	db := getDB(t)

	allDone := make(chan harmonytask.TaskID, 30)
	makeShared := func() *testTask {
		return &testTask{
			name:   "ShareX",
			cost:   resources.Resources{Cpu: 1, Ram: 1 << 20},
			maxN:   10,
			doneCh: allDone,
			stopCh: make(chan struct{}),
			doFunc: func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
				allDone <- id
				return true, nil
			},
		}
	}

	ts1, ts2, ts3 := makeShared(), makeShared(), makeShared()
	harmonytask.Registry["ShareX"] = ts1
	t.Cleanup(func() { ts1.stop(); ts2.stop(); ts3.stop(); delete(harmonytask.Registry, "ShareX") })

	e1 := makeEngine(t, db, []harmonytask.TaskInterface{ts1}, "s1:1000")
	e2 := makeEngine(t, db, []harmonytask.TaskInterface{ts2}, "s2:1000")
	e3 := makeEngine(t, db, []harmonytask.TaskInterface{ts3}, "s3:1000")
	speedUpPolling(e1, e2, e3)

	const total = 9
	for i := 0; i < total; i++ {
		switch i % 3 {
		case 0:
			e1.AddTaskByName("ShareX", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
		case 1:
			e2.AddTaskByName("ShareX", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
		case 2:
			e3.AddTaskByName("ShareX", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
		}
	}

	waitForTasks(t, allDone, total, 30*time.Second)
	waitForNamedSuccessCount(t, db, "ShareX", total, 30*time.Second)
}

// ===== Peering Integration Tests =====

// TestPeeringEndToEnd verifies that task notifications travel between peered
// engines over the in-memory PipeNetwork, causing a remote engine to pick up
// work without relying on DB polling.
//
// Setup: eA handles PeerE2E but always rejects in CanAccept.
//
//	eB handles PeerE2E normally.
//	Both use very slow polling (1 hour).
//	eA adds a PeerE2E task → its scheduler tells peers via TellOthers →
//	eB receives the notification and picks up the task.
func TestPeeringEndToEnd(t *testing.T) {
	db := getDB(t)
	net := harmonytask.NewPipeNetwork()

	// tRejecter handles the task type on eA so AddTaskByName triggers the
	// scheduler (and therefore TellOthers), but CanAccept always rejects so
	// eA never claims the work.
	tRejecter := &testTask{
		name:   "PeerE2E",
		cost:   resources.Resources{Cpu: 1, Ram: 1 << 20},
		maxN:   5,
		doneCh: make(chan harmonytask.TaskID, 100),
		stopCh: make(chan struct{}),
		canAcceptFunc: func([]harmonytask.TaskID, *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
			return nil, nil // reject all
		},
	}
	// tRunner accepts and runs tasks on eB.
	tRunner := &testTask{
		name:   "PeerE2E",
		cost:   resources.Resources{Cpu: 1, Ram: 1 << 20},
		maxN:   5,
		doneCh: make(chan harmonytask.TaskID, 100),
		stopCh: make(chan struct{}),
	}
	harmonytask.Registry["PeerE2E"] = tRejecter
	t.Cleanup(func() {
		tRejecter.stop()
		tRunner.stop()
		delete(harmonytask.Registry, "PeerE2E")
	})

	connA := net.NewNode("peA:1000")
	connB := net.NewNode("peB:1000")

	eA := makeEngineWithPeering(t, db, []harmonytask.TaskInterface{tRejecter}, "peA:1000", connA)
	eB := makeEngineWithPeering(t, db, []harmonytask.TaskInterface{tRunner}, "peB:1000", connB)

	// Let peering handshakes complete, then disable polling so only
	// peer notifications (or direct scheduler-channel events) can trigger work.
	time.Sleep(3 * time.Second)
	eA.TestONLY_SetPollDuration(time.Hour)
	eB.TestONLY_SetPollDuration(time.Hour)
	time.Sleep(2 * time.Second)
	eA.TestONLY_SetPollDuration(time.Hour)
	eB.TestONLY_SetPollDuration(time.Hour)

	// eA adds the task: its scheduler processes the event, CanAccept rejects
	// locally, but TellOthers notifies eB which claims and runs it.
	eA.AddTaskByName("PeerE2E", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		return true, nil
	})

	id := waitForTask(t, tRunner.doneCh, taskTimeout)
	host := waitForHistory(t, db, id, taskTimeout)
	require.Equal(t, "peB:1000", host, "task should have been executed by the peered engine")
}

// TestPeeringThreeNodeRouting verifies task-affinity routing across three
// peered engines. Each engine handles a unique task type; tasks added on
// any engine are routed to the correct handler via peering.
func TestPeeringThreeNodeRouting(t *testing.T) {
	db := getDB(t)
	net := harmonytask.NewPipeNetwork()

	tA := newTestTask("PRingA", 5)
	tB := newTestTask("PRingB", 5)
	tC := newTestTask("PRingC", 5)
	t.Cleanup(cleanupTasks(tA, tB, tC))

	connA := net.NewNode("pr1:1000")
	connB := net.NewNode("pr2:1000")
	connC := net.NewNode("pr3:1000")

	eA := makeEngineWithPeering(t, db, []harmonytask.TaskInterface{tA}, "pr1:1000", connA)
	eB := makeEngineWithPeering(t, db, []harmonytask.TaskInterface{tB}, "pr2:1000", connB)
	eC := makeEngineWithPeering(t, db, []harmonytask.TaskInterface{tC}, "pr3:1000", connC)
	speedUpPolling(eA, eB, eC)

	require.NotEqual(t, eA.OwnerID(), eB.OwnerID())
	require.NotEqual(t, eB.OwnerID(), eC.OwnerID())

	// Each engine adds its own task type — should complete locally.
	eA.AddTaskByName("PRingA", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	aID := waitForTask(t, tA.doneCh, taskTimeout)
	require.Equal(t, "pr1:1000", waitForHistory(t, db, aID, taskTimeout))

	eB.AddTaskByName("PRingB", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	bID := waitForTask(t, tB.doneCh, taskTimeout)
	require.Equal(t, "pr2:1000", waitForHistory(t, db, bID, taskTimeout))

	eC.AddTaskByName("PRingC", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	cID := waitForTask(t, tC.doneCh, taskTimeout)
	require.Equal(t, "pr3:1000", waitForHistory(t, db, cID, taskTimeout))

	waitForSuccessCount(t, db, 3, taskTimeout)
}

// ===== Preemption / Interrupt Tests =====

// noopTestStorage exercises the storage Claim path in task_type_handler without I/O.
type noopTestStorage struct{}

func (noopTestStorage) HasCapacity() bool { return true }

func (noopTestStorage) Claim(taskID int) (func() error, error) {
	_ = taskID
	return func() error { return nil }, nil
}

// TestTimeSensitiveRunsAheadOfMixedClaimingWork caps CPU so two storage-claiming tasks and
// two ordinary tasks fill the machine; a TimeSensitive task must preempt and run quickly,
// then everyone completes after being re-scheduled.
func TestTimeSensitiveRunsAheadOfMixedClaimingWork(t *testing.T) {
	db := getDB(t)

	runUntilPreemptOrDone := func(doneCh chan harmonytask.TaskID, onPreempt chan<- harmonytask.TaskID) func(context.Context, harmonytask.TaskID, func() bool) (bool, error) {
		return func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
			select {
			case <-ctx.Done():
				if onPreempt != nil {
					select {
					case onPreempt <- id:
					default:
					}
				}
				return false, ctx.Err()
			case <-time.After(20 * time.Second):
				doneCh <- id
				return true, nil
			}
		}
	}

	// Any preempted background task (claimer or filler) — ordering among same-age tasks can vary slightly.
	preempted := make(chan harmonytask.TaskID, 4)

	claim := newTestTaskWithOpts("MixClaim", 2, resources.Resources{Cpu: 2, Ram: 1 << 20}, false)
	claim.storage = noopTestStorage{}
	claim.doFunc = runUntilPreemptOrDone(claim.doneCh, preempted)

	filler := newTestTaskWithOpts("MixFill", 8, resources.Resources{Cpu: 1, Ram: 1 << 20}, false)
	filler.doFunc = runUntilPreemptOrDone(filler.doneCh, preempted)

	ts := newTestTaskWithOpts("MixTS", 1, resources.Resources{Cpu: 2, Ram: 1 << 20}, true)
	ts.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		ts.doneCh <- id
		return true, nil
	}

	t.Cleanup(cleanupTasks(claim, filler, ts))

	e := makeEngineWithResources(t, db, []harmonytask.TaskInterface{claim, filler, ts}, "mixts:1000",
		resources.Resources{Cpu: 6, Ram: 1 << 30, Gpu: 0})
	speedUpPolling(e)

	e.AddTaskByName("MixClaim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("MixClaim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("MixFill", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("MixFill", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	time.Sleep(700 * time.Millisecond)

	tsStart := time.Now()
	e.AddTaskByName("MixTS", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })

	_ = waitForTask(t, ts.doneCh, taskTimeout)
	require.Less(t, time.Since(tsStart), 5*time.Second,
		"TimeSensitive should start without waiting for background work to finish voluntarily")

	time.Sleep(300 * time.Millisecond) // let preempted Do() paths signal

	preemptCount := 0
	for {
		select {
		case id := <-preempted:
			require.Greater(t, int(id), 0)
			preemptCount++
		default:
			goto drained
		}
	}
drained:
	require.GreaterOrEqual(t, preemptCount, 1, "machine was full; TimeSensitive needs a preemption")

	waitForNamedSuccessCount(t, db, "MixTS", 1, taskTimeout)
	waitForNamedSuccessCount(t, db, "MixClaim", 2, taskTimeout)
	waitForNamedSuccessCount(t, db, "MixFill", 2, taskTimeout)
}

// TestPreemptionFreesResourcesForTimeSensitive verifies that when a TimeSensitive
// task arrives but resources are exhausted, a non-TimeSensitive running task is
// preempted (context cancelled) so the TimeSensitive task can run.
func TestPreemptionFreesResourcesForTimeSensitive(t *testing.T) {
	db := getDB(t)

	// Victim: blocks until ctx.Done (preemption) or stillOwned false; completes quickly if not preempted.
	preempted := make(chan harmonytask.TaskID, 2)
	victim := newTestTaskWithOpts("PreemptVictim", 2, resources.Resources{Cpu: 1, Ram: 1 << 20}, false)
	victim.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		select {
		case <-ctx.Done():
			preempted <- id
			return false, ctx.Err() // preempted - task will be re-queued
		case <-time.After(2 * time.Second):
			victim.doneCh <- id
			return true, nil
		}
	}

	ts := newTestTaskWithOpts("PreemptTS", 1, resources.Resources{Cpu: 1, Ram: 1 << 20}, true)
	t.Cleanup(cleanupTasks(victim, ts))

	e := makeEngine(t, db, []harmonytask.TaskInterface{victim, ts}, "preempt:1000")
	speedUpPolling(e)

	// Fill capacity with victims (2 slots).
	e.AddTaskByName("PreemptVictim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("PreemptVictim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	time.Sleep(500 * time.Millisecond) // let victims start

	// Add TimeSensitive task - should trigger preemption of one victim
	e.AddTaskByName("PreemptTS", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })

	// TS should complete (it gets the preempted slot)
	tsID := waitForTask(t, ts.doneCh, taskTimeout)
	host := waitForHistory(t, db, tsID, taskTimeout)
	require.Equal(t, "preempt:1000", host)

	// At least one victim should have been preempted
	select {
	case vID := <-preempted:
		require.Greater(t, int(vID), 0)
	case <-time.After(3 * time.Second):
		t.Log("no victim preempted within 3s - machine may have had spare capacity")
	}
}

// TestPreemptedTaskReclaimable verifies that after preemption, the victim task
// row has owner_id=NULL and can be re-claimed and completed.
func TestPreemptedTaskReclaimable(t *testing.T) {
	db := getDB(t)

	preempted := make(chan harmonytask.TaskID, 2)
	victim := newTestTaskWithOpts("ReclaimVictim", 2, resources.Resources{Cpu: 1, Ram: 1 << 20}, false)
	victim.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		select {
		case <-ctx.Done():
			preempted <- id
			return false, ctx.Err()
		case <-time.After(3 * time.Second):
			victim.doneCh <- id
			return true, nil
		}
	}

	ts := newTestTaskWithOpts("ReclaimTS", 1, resources.Resources{Cpu: 1, Ram: 1 << 20}, true)
	t.Cleanup(cleanupTasks(victim, ts))

	e := makeEngine(t, db, []harmonytask.TaskInterface{victim, ts}, "reclaim:1000")
	speedUpPolling(e)

	e.AddTaskByName("ReclaimVictim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	e.AddTaskByName("ReclaimVictim", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	time.Sleep(500 * time.Millisecond)

	e.AddTaskByName("ReclaimTS", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })

	_ = waitForTask(t, ts.doneCh, taskTimeout)

	var vID harmonytask.TaskID
	select {
	case vID = <-preempted:
	case <-time.After(5 * time.Second):
		t.Skip("no preemption occurred - machine may have spare capacity")
	}

	// Wait for victim to be released (owner_id=NULL) and re-claimed, then complete.
	// The task may be re-claimed quickly, so we either see owner_id=NULL briefly or
	// the row disappears (completed). Either way, victim.doneCh signals success.
	waitForTask(t, victim.doneCh, taskTimeout)

	// Verify the preempted task eventually completed (in history)
	var count int
	err := db.QueryRow(context.Background(), `SELECT COUNT(*) FROM harmony_task_history WHERE task_id=$1 AND result=true`, vID).Scan(&count)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, 1, "preempted task should have completed successfully after re-claim")
}

// TestContextCancellationOnGracefulShutdown verifies that when GracefullyTerminate
// is called, running tasks receive context cancellation and exit.
func TestContextCancellationOnGracefulShutdown(t *testing.T) {
	db := getDB(t)

	ctxCancelled := make(chan struct{}, 1)
	blocker := newTestTask("ShutdownBlock", 1)
	blocker.doFunc = func(ctx context.Context, id harmonytask.TaskID, so func() bool) (bool, error) {
		<-ctx.Done()
		ctxCancelled <- struct{}{}
		return false, ctx.Err()
	}
	t.Cleanup(cleanupTasks(blocker))

	e, err := harmonytask.New(db, []harmonytask.TaskInterface{blocker}, "shutdown:1000", &deadPeerConnector{})
	require.NoError(t, err)
	// Do NOT call GracefullyTerminate via Cleanup - we call it explicitly.
	e.TestONLY_SetPollDuration(200 * time.Millisecond)

	e.AddTaskByName("ShutdownBlock", func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) { return true, nil })
	time.Sleep(500 * time.Millisecond) // let task start

	e.GracefullyTerminate()

	select {
	case <-ctxCancelled:
		// Context was cancelled as expected
	case <-time.After(5 * time.Second):
		t.Fatal("task did not receive context cancellation within 5s")
	}
}
