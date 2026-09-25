//go:build integration && !skiff

package harmonytask

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestAdmissionSQLAcquisitionFence(t *testing.T) {
	ctx, db, other, _ := attemptSQLFixture(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,owner_id,added_by,name)
SELECT n,CURRENT_TIMESTAMP,101,101,'Synthetic' FROM generate_series(1,8) n`)
	require.NoError(t, err)
	store := harmonyTaskAttemptStore{db: db, owner: 101, generations: map[TaskID]int64{1: 0, 2: 0, 3: 0, 4: 0, 5: 0, 6: 0, 7: 0, 8: 0}, token: "attempt-a"}
	owner := func(id int) sql.NullInt64 {
		t.Helper()
		var n sql.NullInt64
		require.NoError(t, db.QueryRow(ctx, "SELECT owner_id FROM harmony_task WHERE id=$1", id).Scan(&n))
		return n
	}
	// Failure before token installation can be safely returned.
	require.NoError(t, store.releaseUnstarted(ctx, 1))
	require.False(t, owner(1).Valid)
	// Simulate commit followed by a lost response: cleanup uses both the
	// acquisition and token, not the error as evidence that no write happened.
	require.NoError(t, store.prepare(ctx, 2, store.token))
	require.NoError(t, store.releaseUnstarted(ctx, 2))
	require.False(t, owner(2).Valid)
	// Same owner recovers to a new generation; stale cleanup and prepare fail.
	require.NoError(t, store.prepare(ctx, 3, store.token))
	var gen int64
	require.NoError(t, other.QueryRow(ctx, RECOVER_TASK_ACQUISITION, 3, 101, 0).Scan(&gen))
	require.Equal(t, int64(1), gen)
	newStore := harmonyTaskAttemptStore{db: other, owner: 101, generations: map[TaskID]int64{3: gen}, token: "attempt-b"}
	require.NoError(t, newStore.prepare(ctx, 3, newStore.token))
	require.NoError(t, store.releaseUnstarted(ctx, 3))
	require.Error(t, store.prepare(ctx, 3, store.token))
	require.Equal(t, int64(101), owner(3).Int64)
	ok, err := store.record(ctx, 3, store.token, time.Now())
	require.NoError(t, err)
	require.False(t, ok)
	// Even a token replacement within a generation is not ours to release.
	require.NoError(t, store.prepare(ctx, 4, "another-token"))
	require.NoError(t, store.releaseUnstarted(ctx, 4))
	require.True(t, owner(4).Valid)
	require.NoError(t, store.prepare(ctx, 5, store.token))
	ok, err = store.record(ctx, 5, store.token, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, store.releaseUnstarted(ctx, 5))
	require.True(t, owner(5).Valid)
	_, err = other.Exec(ctx, "UPDATE harmony_task SET owner_id=102 WHERE id=6")
	require.NoError(t, err)
	require.NoError(t, store.releaseUnstarted(ctx, 6))
	require.Equal(t, int64(102), owner(6).Int64)
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(PREPARE_TASK_ATTEMPT, store.token, 7, 101, 0)
		return false, err
	})
	require.NoError(t, err)
	require.False(t, committed)
	var token sql.NullString
	require.NoError(t, db.QueryRow(ctx, "SELECT attempt_id FROM harmony_task WHERE id=7").Scan(&token))
	require.False(t, token.Valid)
	_, err = other.Exec(ctx, "DELETE FROM harmony_task WHERE id=8")
	require.NoError(t, err)
	require.NoError(t, store.releaseUnstarted(ctx, 8))
	require.Error(t, store.prepare(ctx, 8, store.token))
	cancelled, stop := context.WithCancel(ctx)
	stop()
	require.Error(t, store.prepare(cancelled, 7, store.token))
}

// PostgreSQL's linked blocking-PID observation is deliberately not claimed to
// have identical Yugabyte semantics. Runtime statements remain portable.
func TestAdmissionSQLRowLockProgress(t *testing.T) {
	for _, phase := range []string{"preparation", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			ctx, db, other, conn := attemptSQLFixture(t)
			var version string
			require.NoError(t, conn.QueryRow(ctx, "SELECT version()").Scan(&version))
			if strings.Contains(strings.ToLower(version), "yugabyte") {
				t.Skip("linked PostgreSQL lock observer: Yugabyte evidence requires its supported observer")
			}
			_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,added_by,name) VALUES (1,CURRENT_TIMESTAMP,101,'Slow'),(2,CURRENT_TIMESTAMP,101,'Fast')`)
			require.NoError(t, err)
			e, cancel := newAdmissionEngine(t, 4)
			e.cfg.db = db
			e.cfg.ownerID = 101
			finish := make(chan struct{})
			a, slow := addAdmissionHandler(e, "Slow", taskhelp.Max(1), nil, finish)
			b, fast := addAdmissionHandler(e, "Fast", taskhelp.Max(1), nil, finish)
			b.TimeSensitive = true
			b.admissionFactory = nil // Real production claim/adapter.
			atPhase, allowSQL := make(chan struct{}), make(chan struct{})
			var allowOnce sync.Once
			allow := func() { allowOnce.Do(func() { close(allowSQL) }) }
			a.admissionFactory = func(_ string, tasks []task) (func([]TaskID, int) ([]TaskID, error), taskAttemptStore) {
				gens := map[TaskID]int64{}
				store := harmonyTaskAttemptStore{db: db, owner: 101, generations: gens}
				wrapped := admissionPhaseStore{taskAttemptStore: store}
				wrapped.prepareFn = func(c context.Context, id TaskID, token string) error {
					store.token = token
					if phase == "cleanup" {
						if err := store.prepare(c, id, token); err != nil {
							return err
						}
					}
					close(atPhase)
					<-allowSQL
					if phase == "cleanup" {
						return errors.New("injected response loss after committed preparation")
					}
					return store.prepare(c, id, token)
				}
				wrapped.releaseFn = func(c context.Context, id TaskID) error { return store.releaseUnstarted(c, id) }
				return func(ids []TaskID, limit int) ([]TaskID, error) {
					return a.claimTaskOwnership(ids, limit, gens, tasks...)
				}, wrapped
			}
			loopDone := make(chan struct{})
			var locker pgx.Tx
			t.Cleanup(func() {
				allow()
				cancel()
				close(finish)
				if locker != nil {
					c, stop := context.WithTimeout(context.Background(), 5*time.Second)
					_ = locker.Rollback(c)
					stop()
				}
				receiveAdmission(t, loopDone)
				for _, h := range e.handlers {
					for _, a := range h.admissions {
						receiveAdmission(t, a.workerDone)
					}
				}
			})
			go func() { defer close(loopDone); e.runScheduler() }()
			e.schedulerChannel <- schedulerEvent{Source: schedulerSourceDBPoll, DBTasks: map[string][]task{"Slow": {{ID: 1}}}}
			receiveAdmission(t, atPhase) // Claim has committed; not SKIP LOCKED avoidance.
			locker, err = conn.Begin(ctx)
			require.NoError(t, err)
			_, err = locker.Exec(ctx, "SELECT id FROM harmony_task WHERE id=1 FOR UPDATE")
			require.NoError(t, err)
			allow()
			observeCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			for {
				var waiting bool
				require.NoError(t, conn.QueryRow(observeCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE 'UPDATE harmony_task%')`).Scan(&waiting))
				if waiting {
					break
				}
				select {
				case <-observeCtx.Done():
					t.Fatal("contention not established before phase timeout")
				case <-time.After(time.Millisecond * 5):
				}
			}
			// Independent DB connection and actual scheduler both progress while
			// the first row lock is held. This is not a database-outage guarantee.
			var count int
			require.NoError(t, other.QueryRow(ctx, "SELECT count(*) FROM harmony_task WHERE id=2").Scan(&count))
			require.Equal(t, 1, count)
			e.schedulerChannel <- schedulerEvent{Source: schedulerSourcePeerNewTask, TaskType: "Fast", TaskID: 2}
			require.Equal(t, TaskID(2), receiveAdmission(t, fast.entered))
			require.Empty(t, slow.entered)
			require.NoError(t, locker.Rollback(ctx))
			locker = nil
			if phase == "preparation" {
				require.Equal(t, TaskID(1), receiveAdmission(t, slow.entered))
			}
			t.Logf("%s: claim committed, linked row wait observed, independent SQL and Fast Do progressed before lock release", phase)
		})
	}
}
