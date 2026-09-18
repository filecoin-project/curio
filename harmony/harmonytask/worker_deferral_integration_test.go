//go:build integration && !skiff

package harmonytask

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestWorkerUnavailableSQLOrdinaryProbeSpendsBudget(t *testing.T) {
	ctx, db, _ := porepLifecycleDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,owner_id,added_by,posted_time,retries) VALUES(1,'PoRep',101,101,CURRENT_TIMESTAMP,8)`)
	require.NoError(t, err)
	identity := prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "failed-backend")
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}}, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), MaxFailures: 10}}
	now := time.Now()
	gate := taskhelp.NewWorkerBackoff(func() time.Time { return now })
	cause := &taskhelp.WorkerUnavailable{Cause: errors.New("fixture local C2 unavailable")}
	gate.Result(gate.Epoch(), cause)
	require.Equal(t, 8, h.recordCompletion(1, nil, time.Now(), false, cause, false, identity).retry.Retries)
	now = now.Add(2 * time.Minute)
	start, release, ok := gate.Reserve()
	require.True(t, ok)
	defer release()
	require.NoError(t, start(context.Background()))
	_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=101 WHERE id=1`)
	require.NoError(t, err)
	identity = prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "ordinary-probe")
	ordinary := errors.New("invalid proof")
	require.Equal(t, 2*time.Minute, gate.Result(gate.Epoch(), ordinary))
	retry := h.recordCompletion(1, nil, time.Now(), false, ordinary, false, identity).retry
	require.NotNil(t, retry)
	require.Equal(t, 9, retry.Retries)
	var failures int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE NOT result`).Scan(&failures))
	require.Equal(t, 2, failures)
	require.True(t, gate.Blocked())
}

func TestWorkerUnavailableSQLPreservesBudgetAndAttemptIdentity(t *testing.T) {
	ctx, db, _ := porepLifecycleDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,owner_id,added_by,posted_time,retries) VALUES(1,'PoRep',101,101,CURRENT_TIMESTAMP,9)`)
	require.NoError(t, err)
	identity := prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "first-attempt")
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101, hostAndPort: "fixture.example"}}, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), MaxFailures: 10}}
	cause := &taskhelp.WorkerUnavailable{Cause: errors.New("fixture local backend unavailable")}
	retry := h.recordCompletion(1, nil, time.Now(), false, cause, false, identity).retry
	require.NotNil(t, retry)
	require.Equal(t, 9, retry.Retries)
	var failures int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE NOT result`).Scan(&failures))
	require.Equal(t, 1, failures, "failure evidence must remain")
	// Real owner-change trigger clears the previous token. The new owner must
	// prepare its attempt in a subsequent statement, as production does.
	_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=102 WHERE id=1`)
	require.NoError(t, err)
	nextIdentity := prepareRetryFixtureAttempt(t, ctx, db, 102, 1, "new-attempt")
	require.False(t, h.recordCompletion(1, nil, time.Now(), false, cause, false, identity).applied)
	var owner int
	var token string
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_id,attempt_id FROM harmony_task WHERE id=1`).Scan(&owner, &token))
	require.Equal(t, 102, owner)
	require.Equal(t, "new-attempt", token)
	// Ordinary invalid-sector failure still spends the existing final attempt.
	h.TaskEngine.cfg.ownerID = 102
	h.recordCompletion(1, nil, time.Now(), false, errors.New("invalid sector proof"), false, nextIdentity)
	var remaining int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task`).Scan(&remaining))
	require.Zero(t, remaining)
}
