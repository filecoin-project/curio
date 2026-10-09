//go:build integration && !skiff

package harmonytask

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestCompletionSQLSameOwnerRecoveryAndMissing(t *testing.T) {
	ctx, db, other, _ := attemptSQLFixture(t)
	for _, change := range []string{"same-owner-recovery", "generation-only", "token-only", "missing"} {
		t.Run(change, func(t *testing.T) {
			for i, mode := range []string{"success", "retry", "terminal", "preemption"} {
				t.Run(mode, func(t *testing.T) {
					id := TaskID(i + 1)
					_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,name,added_by,owner_id,retries)
 VALUES($1,CURRENT_TIMESTAMP,'TreeRC',101,101,2)`, id)
					require.NoError(t, err)
					identity := prepareRetryFixtureAttempt(t, ctx, db, 101, id, "old-token")
					h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}},
						TaskTypeDetails: TaskTypeDetails{Name: "TreeRC", Max: taskhelp.Max(1), MaxFailures: 3}}
					switch change {
					case "same-owner-recovery", "generation-only":
						peer := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: other, ownerID: 101}}}
						generations := map[TaskID]int64{}
						ids, err := peer.recoverTaskOwnership([]task{{ID: id, OwnerGeneration: identity.generation}}, []TaskID{id}, 1, generations)
						require.NoError(t, err)
						require.Equal(t, []TaskID{id}, ids)
						token := "new-token"
						if change == "generation-only" {
							token = "old-token"
						} // Predicate isolation, not production token generation.
						prepareRetryFixtureAttempt(t, ctx, other, 101, id, token)
					case "token-only":
						_, err = other.Exec(ctx, `UPDATE harmony_task SET attempt_id='different-token' WHERE id=$1`, id)
						require.NoError(t, err)
					case "missing":
						_, err = other.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, id)
						require.NoError(t, err)
					}
					read := func() string {
						var snapshot string
						require.NoError(t, db.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(h)),'[]'::jsonb)::text FROM harmony_task h WHERE id=$1`, id).Scan(&snapshot))
						return snapshot
					}
					before := read()
					outcome := errors.New("failure")
					if mode == "success" {
						outcome = nil
					}
					if mode == "retry" {
						h.MaxFailures = 10
					}
					if mode == "preemption" {
						outcome = context.Canceled
					}
					start := time.Now()
					result := h.recordCompletion(id, nil, time.Now(), mode == "success", outcome, mode == "preemption", identity)
					require.Less(t, time.Since(start), time.Second, "stale/missing is not a database-error retry loop")
					require.False(t, result.applied)
					require.Nil(t, result.retry)
					require.Equal(t, before, read())
					var history int
					require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history`).Scan(&history))
					require.Zero(t, history)
					_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, id)
					require.NoError(t, err)
				})
			}
		})
	}
}

// A failed history INSERT must roll the task mutation back. A test-owned sequence
// survives rollback so precisely the first INSERT raises a serialization error;
// recordCompletion then retries the actual transaction, not a copied SQL model.
func TestCompletionSQLHistoryRollbackRetry(t *testing.T) {
	ctx, db, _, conn := attemptSQLFixture(t)
	_, err := conn.Exec(ctx, `CREATE SEQUENCE completion_fixture_attempt;
 CREATE FUNCTION fail_first_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF nextval('completion_fixture_attempt')=1 THEN RAISE EXCEPTION 'fixture retry' USING ERRCODE='40001'; END IF;
 RETURN NEW; END $$;
 CREATE TRIGGER completion_fixture BEFORE INSERT ON harmony_task_history FOR EACH ROW EXECUTE FUNCTION fail_first_completion();`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_task(id,name,posted_time,added_by,owner_id,retries)
 VALUES(1,'TreeRC',CURRENT_TIMESTAMP,101,101,1)`)
	require.NoError(t, err)
	identity := prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "retry-once")
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}}, TaskTypeDetails: TaskTypeDetails{Name: "TreeRC", Max: taskhelp.Max(1), MaxFailures: 3}}
	result := h.recordCompletion(1, nil, time.Now(), false, errors.New("actual task failure"), false, identity)
	require.True(t, result.applied)
	require.NotNil(t, result.retry)
	require.Equal(t, 2, result.retry.Retries, "rolled-back attempt must not consume two failures")
	var retries, history, attempts int
	require.NoError(t, db.QueryRow(ctx, `SELECT retries FROM harmony_task WHERE id=1`).Scan(&retries))
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=1`).Scan(&history))
	require.NoError(t, db.QueryRow(ctx, `SELECT last_value FROM completion_fixture_attempt`).Scan(&attempts))
	require.Equal(t, 2, retries)
	require.Equal(t, 1, history)
	require.Equal(t, 2, attempts)
}

func TestCompletionSQLWaitsForAcquisitionChange(t *testing.T) {
	ctx, db, observer, conn := attemptSQLFixture(t)
	var version string
	require.NoError(t, conn.QueryRow(ctx, `SELECT version()`).Scan(&version))
	if strings.Contains(strings.ToLower(version), "yugabyte") {
		t.Skip("PostgreSQL linked blocker observer; no claim of equivalent Yugabyte observation")
	}
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,posted_time,added_by,owner_id,retries)
 VALUES(1,'TreeRC',CURRENT_TIMESTAMP,101,101,2)`)
	require.NoError(t, err)
	identity := prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "before-handoff")
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}}, TaskTypeDetails: TaskTypeDetails{Name: "TreeRC", Max: taskhelp.Max(1), MaxFailures: 3}}
	locker, err := conn.Begin(ctx)
	require.NoError(t, err)
	finished := make(chan taskCompletion, 1)
	joined := make(chan struct{})
	started := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = locker.Rollback(cleanup) // Always release the holder BEFORE joining.
		if started {
			select {
			case <-joined:
			case <-cleanup.Done():
				t.Error("completion participant not joined")
			}
		}
	}()
	_, err = locker.Exec(ctx, `SELECT id FROM harmony_task WHERE id=1 FOR UPDATE`)
	require.NoError(t, err)
	started = true
	go func() {
		defer close(joined)
		finished <- h.recordCompletion(1, nil, time.Now(), false, errors.New("late failure"), false, identity)
	}()
	require.Eventually(t, func() bool {
		var linked bool
		err := observer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
 WHERE $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock'
 AND query LIKE 'SELECT posted_time, update_time, retries FROM harmony_task%')`, conn.PgConn().PID()).Scan(&linked)
		return err == nil && linked
	}, time.Second, 5*time.Millisecond, "contention not established")
	t.Log("observed completion waiting on this fixture's exact acquisition row-lock holder")
	_, err = locker.Exec(ctx, `UPDATE harmony_task SET owner_id=102 WHERE id=1`)
	require.NoError(t, err)
	require.NoError(t, locker.Commit(ctx))
	select {
	case result := <-finished:
		require.False(t, result.applied, "post-wait recheck must reject the stale terminal deletion")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var owner, retries, history int
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_id,retries FROM harmony_task WHERE id=1`).Scan(&owner, &retries))
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history`).Scan(&history))
	require.Equal(t, 102, owner)
	require.Equal(t, 2, retries)
	require.Zero(t, history)
}
