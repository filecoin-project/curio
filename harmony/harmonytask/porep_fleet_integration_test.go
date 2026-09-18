//go:build integration && !skiff

package harmonytask

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

// Native/chain work is replaced at Do by complete loopback HTTP input and a
// finite timer. One millisecond represents one second. Ownership, attempts,
// scheduler loop, peer JSON, backoff, completion/history and events are real.
// The production 10ms bundler is intentionally NOT accelerated.
type fleetTask struct {
	stubAcceptTask
	db       *harmonydb.DB
	health   *taskhelp.WorkerBackoff
	isolate  bool
	faulty   atomic.Bool
	duration time.Duration
	url      string
	client   *http.Client
	entries  atomic.Int64
	returns  atomic.Int64
	stop     context.Context
}

func (f *fleetTask) TaskStartBlocked() bool { return f.isolate && f.health.Blocked() }
func (f *fleetTask) ReserveTaskStart(TaskID) (func(context.Context) error, func(), bool) {
	if !f.isolate {
		return nil, nil, true
	}
	return f.health.Reserve()
}
func (f *fleetTask) GetSectorID(db *harmonydb.DB, id int64) (*abi.SectorID, error) {
	var s abi.SectorID
	err := db.QueryRow(f.stop, `SELECT sp_id,sector_number FROM sectors_sdr_pipeline WHERE task_id_porep=$1`, id).Scan(&s.Miner, &s.Number)
	return &s, err
}
func (f *fleetTask) Do(ctx context.Context, id TaskID, _ func() bool) (bool, error) {
	f.entries.Add(1)
	defer f.returns.Add(1)
	epoch := f.health.Epoch()
	bad := f.faulty.Load()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return false, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return false, err
	}
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return false, err
	}
	delay := f.duration
	if bad {
		delay = 4 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-f.stop.Done():
		return false, f.stop.Err()
	}
	if bad {
		err := errors.New("No CUDA devices available")
		if f.isolate {
			err = &taskhelp.WorkerUnavailable{Cause: err}
			f.health.Result(epoch, err)
		}
		return false, err
	}
	if f.isolate {
		f.health.Result(epoch, nil)
	}
	_, err = f.db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_porep=TRUE,task_id_porep=NULL WHERE task_id_porep=$1`, id)
	return err == nil, err
}

type fleetPeer struct{ destination *peering }

func (c fleetPeer) SendMessage(b []byte) error {
	return c.destination.handlePeerMessage("fixture.example", 1, b)
}
func (fleetPeer) ReceiveMessage() ([]byte, error) { return nil, io.EOF }
func (fleetPeer) Close() error                    { return nil }

func TestPoRepFleetSQL(t *testing.T) {
	mode := os.Getenv("CURIO_POREP_FLEET_MODE")
	if mode == "" {
		mode = "isolated"
	}
	require.Contains(t, []string{"healthy", "baseline", "isolated", "all-faulty", "one-healthy", "rejoin"}, mode)
	for _, poll := range []time.Duration{30 * time.Millisecond, 3 * time.Millisecond} {
		t.Run(fmt.Sprintf("%s-poll-%s", mode, poll), func(t *testing.T) { runPoRepFleetSQL(t, mode, poll) })
	}
}

func runPoRepFleetSQL(t *testing.T, mode string, poll time.Duration) {
	var cfg harmonydb.Config
	ctx, first, second := porepLifecycleDB(t, func(c harmonydb.Config) { cfg = c })
	dbs := []*harmonydb.DB{first, second}
	for len(dbs) < 6 {
		d, err := harmonydb.NewFromConfig(cfg)
		require.NoError(t, err)
		t.Cleanup(d.ITestDeleteAll)
		dbs = append(dbs, d)
	}
	for i := 2; i < 6; i++ {
		_, err := first.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu) VALUES($1,$2,8,1024,1)`, 101+i, fmt.Sprintf("fixture-%d.example", i))
		require.NoError(t, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "complete synthetic vanilla input")
	}))
	defer server.Close()
	runCtx, stop := context.WithCancel(ctx)
	var engines []*TaskEngine
	var tasks []*fleetTask
	var loops sync.WaitGroup
	clockBase := time.Now()
	clock := func() time.Time { return clockBase.Add(time.Since(clockBase) * 1000) }
	n := 6
	if mode == "healthy" {
		n = 4
	}
	for i := 0; i < n; i++ {
		f := &fleetTask{db: dbs[i], health: taskhelp.NewWorkerBackoff(clock), isolate: mode != "baseline", duration: time.Duration(150+i%4*10) * time.Millisecond, url: server.URL, client: server.Client(), stop: runCtx}
		f.faulty.Store(i >= 4 || mode == "all-faulty" || (mode == "one-healthy" && i > 0))
		e := &TaskEngine{cfg: taskEngineConfig{ctx: runCtx, db: dbs[i], ownerID: 101 + i, hostAndPort: fmt.Sprintf("fixture-%d.example", i), reg: &resources.Reg{Resources: resources.Resources{Cpu: 2, Gpu: 1, Ram: 1024}}}, schedulerChannel: make(chan schedulerEvent, 4096), admissionWake: make(chan struct{}, 1)}
		h := &taskTypeHandler{TaskInterface: f, TaskEngine: e, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), MaxFailures: 10, Cost: resources.Resources{Cpu: 1, Gpu: 1}, RetryWait: func(n int) time.Duration { return min(time.Second<<n, 2*time.Minute) / 1000 }}, running: runregistry.New(), accept: acceptcache.New(time.Second), storageFailures: map[TaskID]time.Time{}}
		e.handlers = []*taskTypeHandler{h}
		e.taskMap = map[string]*taskTypeHandler{"PoRep": h}
		e.peering = &peering{h: e, peers: peerregistry.New()}
		engines = append(engines, e)
		tasks = append(tasks, f)
	}
	for i, e := range engines {
		for j, dest := range engines {
			if i != j {
				e.peering.peers.Add(int64(j+1), fmt.Sprintf("peer-%d.example", j), fleetPeer{dest.peering}, []string{"PoRep"})
			}
		}
	}
	// Release/return native substitutes, then join scheduler/poll producers before
	// fixture schema/pool cleanup. No wait with an unreleased native barrier.
	t.Cleanup(func() {
		stop()
		loops.Wait()
		// runScheduler cancels pending admissions before returning. Join their
		// bounded preparation/cleanup workers before any pool/schema cleanup.
		admissionDeadline := time.NewTimer(12 * time.Second)
		defer admissionDeadline.Stop()
		for _, e := range engines {
			for _, h := range e.handlers {
				for _, admission := range h.admissions {
					select {
					case <-admission.workerDone:
					case <-admissionDeadline.C:
						t.Error("admission participants did not join before fixture cleanup")
						return
					}
				}
			}
		}
		joinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		deadline := time.Now().Add(5 * time.Second)
		for {
			active := int64(0)
			entries := int64(0)
			for _, f := range tasks {
				active += f.entries.Load() - f.returns.Load()
				entries += f.entries.Load()
			}
			var recorded int64
			err := first.QueryRow(joinCtx, `SELECT count(*) FROM harmony_task_history`).Scan(&recorded)
			if active == 0 && err == nil && recorded == entries {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("participants did not join: active=%d entered=%d recorded=%d query=%v", active, entries, recorded, err)
				break
			}
			time.Sleep(time.Millisecond)
		}
	})
	insert := func(firstID, lastID int) {
		for id := firstID; id <= lastID; id++ {
			_, err := first.Exec(ctx, `INSERT INTO harmony_task(id,name,posted_time,added_by) VALUES($1,'PoRep',CURRENT_TIMESTAMP,101)`, id)
			require.NoError(t, err)
			_, err = first.Exec(ctx, `INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,task_id_porep) VALUES(1000,$1,0,$1)`, id)
			require.NoError(t, err)
		}
	}
	insert(1, 24)
	for i, e := range engines {
		loops.Add(2)
		go func() { defer loops.Done(); e.runScheduler() }()
		go func() {
			defer loops.Done()
			phase := time.NewTimer(time.Duration(i) * time.Millisecond)
			defer phase.Stop()
			select {
			case <-phase.C:
			case <-runCtx.Done():
				return
			}
			ticker := time.NewTicker(poll)
			defer ticker.Stop()
			for {
				snapshot := e.pollAllTaskTypes()
				select {
				case e.schedulerChannel <- schedulerEvent{Source: schedulerSourceDBPoll, DBTasks: snapshot}:
				case <-runCtx.Done():
					return
				}
				select {
				case <-ticker.C:
				case <-runCtx.Done():
					return
				}
			}
		}()
	}
	// Twenty new tasks per simulated 1000 seconds is below four workers' 24+
	// capacity; the initial backlog tests the adversarial retry competition.
	for next := 25; next <= 40; next += 2 {
		time.Sleep(100 * time.Millisecond)
		insert(next, next+1)
	}
	if mode == "rejoin" {
		for _, f := range tasks {
			f.faulty.Store(false)
		}
	}
	deadline := time.Now().Add(8 * time.Second)
	var remaining, completed, terminal int
	for {
		require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM harmony_task`).Scan(&remaining))
		if remaining == 0 || mode == "all-faulty" && time.Now().After(deadline.Add(-6*time.Second)) {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Do has returned before Max/DB completion. Wait for all history writes, not
	// merely Max=0, before inspecting final state or closing pools.
	for time.Now().Before(deadline) {
		var history int
		require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history`).Scan(&history))
		var entries int64
		for _, f := range tasks {
			entries += f.entries.Load()
		}
		if int64(history) == entries {
			break
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM sectors_sdr_pipeline WHERE after_porep`).Scan(&completed))
	require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM sectors_sdr_pipeline p LEFT JOIN harmony_task t ON t.id=p.task_id_porep WHERE NOT after_porep AND t.id IS NULL`).Scan(&terminal))
	var failures, repeated int
	var latency, crossHost float64
	require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE NOT result`).Scan(&failures))
	require.NoError(t, first.QueryRow(ctx, `SELECT coalesce(sum(n-1),0) FROM (SELECT count(*) n FROM harmony_task_history GROUP BY task_id,completed_by_host_and_port HAVING count(*)>1) q`).Scan(&repeated))
	require.NoError(t, first.QueryRow(ctx, `SELECT coalesce(max(extract(epoch FROM started-ended)),0) FROM (SELECT work_end ended,lead(work_start) OVER(PARTITION BY task_id ORDER BY work_start,id) started FROM harmony_task_history) q`).Scan(&latency))
	require.NoError(t, first.QueryRow(ctx, `SELECT coalesce(max(extract(epoch FROM started-ended)),0) FROM (SELECT work_end ended,completed_by_host_and_port host,lead(work_start) OVER w started,lead(completed_by_host_and_port) OVER w next_host FROM harmony_task_history WINDOW w AS (PARTITION BY task_id ORDER BY work_start,id)) q WHERE host<>next_host`).Scan(&crossHost))
	var hosts []struct {
		Host    string `db:"host"`
		Success int    `db:"success"`
		Failure int    `db:"failure"`
	}
	require.NoError(t, first.Select(ctx, &hosts, `SELECT completed_by_host_and_port host,count(*) FILTER (WHERE result) success,count(*) FILTER (WHERE NOT result) failure FROM harmony_task_history GROUP BY completed_by_host_and_port ORDER BY completed_by_host_and_port`))
	t.Logf("FLEET mode=%s poll=%s scale=1000 tasks=40 completed=%d failures=%d same_host_repeats=%d terminal_pipeline=%d backlog=%d max_next_attempt_seconds=%.3f max_cross_host_seconds=%.3f elapsed_simulated_seconds=%.3f hosts=%+v", mode, poll, completed, failures, repeated, terminal, remaining, latency*1000, crossHost*1000, time.Since(clockBase).Seconds()*1000, hosts)
	if mode == "rejoin" {
		// Whole-fleet completion alone can pass even when neither repaired
		// worker rejoins. Require committed success history for BOTH former
		// faulty owners, not merely their Do-entry or gate-state observation.
		for _, owner := range []string{"fixture-4.example", "fixture-5.example"} {
			successes, priorFailures := 0, 0
			for _, host := range hosts {
				if host.Host == owner {
					successes, priorFailures = host.Success, host.Failure
				}
			}
			require.Positive(t, priorFailures, "rejoin fixture must first observe failure on %s", owner)
			require.Positive(t, successes, "repaired owner %s must commit successful history; healthy-only completion is not rejoin", owner)
		}
	}
	require.Equal(t, 40, completed+terminal+remaining, "queue shrink is not success")
	if mode != "baseline" {
		require.Zero(t, terminal)
		if mode != "all-faulty" {
			require.Equal(t, 40, completed)
			require.Zero(t, remaining)
		} else {
			require.Zero(t, completed)
			require.Equal(t, 40, remaining)
		}
	}
	if mode == "isolated" {
		require.LessOrEqual(t, failures, 8, "bounded worker probes must not storm the backlog")
	}
}
