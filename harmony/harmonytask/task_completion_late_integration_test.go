//go:build integration && !skiff

package harmonytask

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

// Characterization, not a cleanup eligibility test. The held body substitutes
// for uninterruptible native work; no proof computation, chain or storage API
// is invoked. Actual claim, attempt entry and completion SQL are executed.
func TestCompletionSQLLateCompletion(t *testing.T) {
	if os.Getenv("CURIO_CLEANUP_BOUNDARY_ITEST") != "1" {
		t.Skip("requires CURIO_CLEANUP_BOUNDARY_ITEST=1 as well as the explicit task fixture target")
	}
	require.Equal(t, "127.0.0.1", os.Getenv("CURIO_TASK_ATTEMPT_ITEST_HOST"))
	require.Equal(t, "curio_cleanup_disposable", os.Getenv("CURIO_TASK_ATTEMPT_ITEST_DATABASE"))
	require.Equal(t, "curio_cleanup_fixture", os.Getenv("CURIO_TASK_ATTEMPT_ITEST_USER"))
	ctx, db, other, _ := attemptSQLFixture(t)
	for stageIndex, stage := range []string{tasknames.SDR, tasknames.TreeRC} {
		t.Run(stage, func(t *testing.T) {
			for i, mode := range []string{"terminal-failure", "retryable-failure", "success", "preemption", "worker-unavailable"} {
				t.Run(mode, func(t *testing.T) {
					id := TaskID(stageIndex*10 + i + 1)
					_, err := db.Exec(ctx, `INSERT INTO harmony_machines(id,host_and_port,cpu,ram,gpu)
 VALUES(101,'old.example',8,1024,0) ON CONFLICT(id) DO UPDATE SET last_contact=CURRENT_TIMESTAMP`)
					require.NoError(t, err)
					retries := 0
					maxFailures := uint(2) // SDR.TypeDetails; TreeRC has three attempts.
					if stage == tasknames.TreeRC {
						maxFailures = 3
					}
					if mode == "terminal-failure" {
						retries = int(maxFailures) - 1
					}
					_, err = db.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,added_by,name,owner_id,retries)
 VALUES($1,CURRENT_TIMESTAMP,101,$3,101,$2)`, id, retries, stage)
					require.NoError(t, err)
					_, err = db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,task_id_sdr)
 VALUES(1000,$1,0,$1)`, id)
					require.NoError(t, err)
					if stage == tasknames.TreeRC {
						_, err = db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_sdr=TRUE,after_tree_d=TRUE,
 task_id_sdr=NULL,task_id_tree_c=$1,task_id_tree_r=$1 WHERE sp_id=1000 AND sector_number=$1`, id)
						require.NoError(t, err)
					}
					identity := prepareRetryFixtureAttempt(t, ctx, db, 101, id, "old-attempt")
					oldStore := harmonyTaskAttemptStore{db: db, owner: 101}
					recorded, err := oldStore.record(ctx, id, "old-attempt", time.Now().Add(-time.Minute))
					require.NoError(t, err)
					require.True(t, recorded)
					old := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101, hostAndPort: "old.example"}},
						TaskTypeDetails: TaskTypeDetails{Name: stage, Max: taskhelp.Max(1), MaxFailures: maxFailures}}
					// Only the heartbeat age is fixture input. The real machine cleanup
					// and FK release are used. A missing heartbeat is not a native-stop
					// acknowledgement, and cordon alone does not perform this transition.
					_, err = other.Exec(ctx, `UPDATE harmony_machines SET last_contact=CURRENT_TIMESTAMP-INTERVAL '1 millisecond'*$1 WHERE id=101`, resources.LOOKS_DEAD_TIMEOUT.Milliseconds()+1000)
					require.NoError(t, err)
					require.Equal(t, 1, resources.CleanupMachines(ctx, other))
					var updated time.Time
					require.NoError(t, other.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&updated))
					current := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: other, ownerID: 102}}, TaskTypeDetails: old.TaskTypeDetails}
					generations := map[TaskID]int64{}
					accepted, err := current.claimTaskOwnership([]TaskID{id}, 1, generations, task{ID: id, Retries: retries, UpdateTime: updated})
					require.NoError(t, err)
					require.Equal(t, []TaskID{id}, accepted)
					store := harmonyTaskAttemptStore{db: other, owner: 102, generations: generations}
					require.NoError(t, store.prepare(ctx, id, "current-attempt"))
					entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
					var running atomic.Bool
					var once sync.Once
					defer func() {
						once.Do(func() { close(release) })
						select {
						case <-joined:
						case <-time.After(6 * time.Second):
							t.Error("owned participant did not join before fixture cleanup")
						}
					}()
					go func() {
						defer close(joined)
						_, _ = runWithAttemptStart(ctx, store, id, "current-attempt", nil, time.Now, func(time.Time) {}, func() (bool, error) {
							running.Store(true)
							close(entered)
							select {
							case <-release:
							case <-ctx.Done():
							}
							running.Store(false)
							return false, errors.New("fixture body released; no completion callback")
						})
					}()
					select {
					case <-entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					// Wait for the real asynchronous entry write, independently of the
					// held body. The stale completion below cannot be an entry-write race.
					require.Eventually(t, func() bool {
						var started bool
						err := other.QueryRow(ctx, `SELECT attempt_started_at IS NOT NULL AND attempt_id='current-attempt' FROM harmony_task WHERE id=$1`, id).Scan(&started)
						return err == nil && started
					}, 5*time.Second, 5*time.Millisecond)
					var before, after string
					require.NoError(t, db.QueryRow(ctx, `SELECT to_jsonb(h)::text FROM harmony_task h WHERE id=$1`, id).Scan(&before))
					outcome := errors.New("late old invocation")
					if mode == "success" {
						outcome = nil
					}
					if mode == "preemption" {
						outcome = context.Canceled
					}
					if mode == "worker-unavailable" {
						outcome = &taskhelp.WorkerUnavailable{Cause: outcome}
					}
					result := old.recordCompletion(id, &abi.SectorID{Miner: 1000, Number: abi.SectorNumber(id)}, time.Now().Add(-time.Minute), mode == "success", outcome, mode == "preemption", identity)
					var taskPresent, failedHistory bool
					require.NoError(t, db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM harmony_task WHERE id=$1),
 EXISTS(SELECT 1 FROM harmony_task_history WHERE task_id=$1 AND NOT result)`, id).Scan(&taskPresent, &failedHistory))
					t.Logf("stage=%s mode=%s AfterSDR=%t newer body held=%t task present=%t failed history=%t applied=%t",
						stage, mode, stage == tasknames.TreeRC, running.Load(), taskPresent, failedHistory, result.applied)
					require.False(t, result.applied, "old completion must not finalize the still-running newer invocation")
					require.Nil(t, result.retry)
					require.NoError(t, db.QueryRow(ctx, `SELECT to_jsonb(h)::text FROM harmony_task h WHERE id=$1`, id).Scan(&after))
					require.Equal(t, before, after, "retry clock, failure budget and complete acquisition identity must be unchanged")
					var rows, currentOwner, history int
					require.NoError(t, db.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE owner_id=102 AND attempt_id='current-attempt') FROM harmony_task WHERE id=$1`, id).Scan(&rows, &currentOwner))
					require.True(t, running.Load(), "the newer body must still be held")
					require.Equal(t, 1, currentOwner)
					require.Equal(t, 1, rows)
					require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=$1`, id).Scan(&history))
					require.Zero(t, history, "stale diagnostics must not be normal terminal history")
					var linked TaskID
					require.NoError(t, db.QueryRow(ctx, `SELECT COALESCE(task_id_tree_r,task_id_sdr) FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=$1`, id).Scan(&linked))
					require.Equal(t, id, linked)
					var afterSDR, afterTrees, failed, onChain, finalized bool
					require.NoError(t, db.QueryRow(ctx, `SELECT after_sdr,after_tree_c OR after_tree_r,failed,
 after_precommit_msg_success,after_finalize FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=$1`, id).
						Scan(&afterSDR, &afterTrees, &failed, &onChain, &finalized))
					require.Equal(t, stage == tasknames.TreeRC, afterSDR)
					require.False(t, afterTrees)
					require.False(t, onChain)
					require.False(t, finalized)
					require.False(t, failed, "retry exhaustion does not set pipeline.failed; do not manufacture cleanup eligibility")
					// This is deliberately NOT a safe-removal positive control. Current
					// ownership/native body prohibit removal, and a stale failure cannot
					// manufacture terminal history. Even the old defect's missing task
					// would leave native quiescence unknown, not pass all safety guards.
					require.True(t, running.Load())
					t.Logf("mode=%s current body held=true; current ownership rows=%d; task rows=%d; history=%d; pipeline link retained=true", mode, currentOwner, rows, history)
					once.Do(func() { close(release) })
					select {
					case <-joined:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					currentResult := current.recordCompletion(id, nil, time.Now(), mode == "success", outcome, mode == "preemption", completionIdentityFor(store, id, "current-attempt"))
					require.True(t, currentResult.applied, "current invocation must still complete normally")
					// A pipeline-only lock also does not participate in the real task
					// claim query. This uses another independent HarmonyDB pool.
					if mode == "retryable-failure" {
						require.NoError(t, other.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&updated))
						type claimResult struct {
							ids []TaskID
							err error
						}
						claimFinished := make(chan claimResult, 1)
						claimJoined := make(chan struct{})
						claimStarted := false
						defer func() {
							if claimStarted {
								select {
								case <-claimJoined:
								case <-time.After(6 * time.Second):
									t.Error("claim participant not joined before fixture cleanup")
								}
							}
						}()
						committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
							var sector int64
							if err := tx.QueryRow(`SELECT sector_number FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=$1 FOR UPDATE`, id).Scan(&sector); err != nil {
								return false, err
							}
							// HarmonyDB rejects nested non-transaction calls in the same
							// goroutine even when they use another pool. Use an actual
							// independent participant and join it before leaving this scope.
							claimStarted = true
							go func() {
								defer close(claimJoined)
								claimed, err := current.claimTaskOwnership([]TaskID{id}, 1, map[TaskID]int64{}, task{ID: id, Retries: 1, UpdateTime: updated})
								claimFinished <- claimResult{claimed, err}
							}()
							select {
							case result := <-claimFinished:
								require.Equal(t, []TaskID{id}, result.ids, "pipeline row lock did not exclude actual claim")
								return false, result.err
							case <-ctx.Done():
								return false, ctx.Err()
							}
						})
						require.NoError(t, err)
						require.False(t, committed)
					}
				})
			}
		})
	}
}
