//go:build integration && !skiff

package harmonytask

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestRetrySQLNewNotificationDoesNotRequireInventedTimestamp(t *testing.T) {
	ctx, db, _ := porepLifecycleDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,added_by,posted_time,update_time) VALUES(1,'PoRep',101,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP-INTERVAL '1 second')`)
	require.NoError(t, err)
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}}}
	// New-task and legacy notifications have no authoritative update_time.
	// Retry zero has no backoff. Receipt time must not become a false CAS value.
	row := taskFromSchedulerEvent(schedulerEvent{TaskID: 1, Retries: 0})
	ids, err := h.claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
	require.NoError(t, err)
	require.Equal(t, []TaskID{1}, ids, "new task must not wait for a DB poll merely to learn update_time")
}

func TestRetrySQLAuthoritativeClaimAndCompletion(t *testing.T) {
	ctx, db, other := porepLifecycleDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,added_by,posted_time,update_time,retries) VALUES(1,'PoRep',101,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)`)
	require.NoError(t, err)
	hs := make([]*taskTypeHandler, 2)
	for i, conn := range []*harmonydb.DB{db, other} {
		hs[i] = &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: conn, ownerID: 101 + i, hostAndPort: "fixture.example"}}, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), RetryWait: func(n int) time.Duration { return min(time.Second<<n, 2*time.Minute) }}}
	}
	snapshot := func() task {
		var row task
		require.NoError(t, db.QueryRow(ctx, `SELECT id,posted_time,update_time,retries FROM harmony_task WHERE id=1`).Scan(&row.ID, &row.PostedTime, &row.UpdateTime, &row.Retries))
		return row
	}
	row := snapshot()
	ids, err := hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
	require.NoError(t, err)
	require.Empty(t, ids, "DB deadline must be enforced")
	_, err = db.Exec(ctx, `UPDATE harmony_task SET update_time=CURRENT_TIMESTAMP-INTERVAL '5 seconds' WHERE id=1`)
	require.NoError(t, err)
	stale := snapshot()
	_, err = db.Exec(ctx, `UPDATE harmony_task SET retries=2,update_time=CURRENT_TIMESTAMP-INTERVAL '5 seconds' WHERE id=1`)
	require.NoError(t, err)
	ids, err = hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, stale)
	require.NoError(t, err)
	require.Empty(t, ids, "stale retry generation must not claim")
	row = snapshot()
	var equality, due bool
	var zone string
	require.NoError(t, db.QueryRow(ctx, `SELECT update_time=$1::timestamptz, CURRENT_TIMESTAMP>=update_time+INTERVAL '4 seconds',current_setting('TimeZone') FROM harmony_task WHERE id=1`, row.UpdateTime).Scan(&equality, &due, &zone))
	t.Logf("claim snapshot=%+v equality=%v due=%v db_timezone=%s", row, equality, due, zone)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan []TaskID, 2)
	errs := make(chan error, 2)
	for _, h := range hs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, e := h.claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
			results <- got
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	count := 0
	for got := range results {
		count += len(got)
	}
	require.Equal(t, 1, count, "exactly one committed owner")
	var owner int
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id=1`).Scan(&owner))
	identity := prepareRetryFixtureAttempt(t, ctx, db, owner, 1, "retry-fixture")
	retry := hs[owner-101].recordCompletion(1, nil, time.Now(), false, errors.New("sector failure"), false, identity).retry
	require.NotNil(t, retry)
	require.Equal(t, snapshot(), *retry, "emitter must use the DB timestamp, not local Now")
	ids, err = hs[1].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
	require.NoError(t, err)
	require.Empty(t, ids)
	ids, err = hs[1].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, *retry)
	require.NoError(t, err)
	require.Empty(t, ids, "completion backoff must not be bypassed")
}
