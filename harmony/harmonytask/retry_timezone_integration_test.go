//go:build integration && !skiff

package harmonytask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

// Owned roles apply session settings to EVERY connection (including newly
// opened pool connections). SET on one borrowed connection is insufficient.
func retryZonePool(t *testing.T, ctx context.Context, admin *pgx.Conn, cfg harmonydb.Config, suffix, zone string) *harmonydb.DB {
	t.Helper()
	role := "retry_" + string(cfg.ITestID) + "_" + suffix
	identifier := pgx.Identifier{role}.Sanitize()
	_, err := admin.Exec(ctx, "CREATE ROLE "+identifier+" LOGIN IN ROLE "+pgx.Identifier{cfg.Username}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := admin.Exec(cleanup, "DROP ROLE "+identifier)
		require.NoError(t, err)
	})
	_, err = admin.Exec(ctx, "ALTER ROLE "+identifier+" SET TimeZone TO '"+strings.ReplaceAll(zone, "'", "''")+"'")
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "ALTER ROLE "+identifier+" SET statement_timeout TO '5000'")
	require.NoError(t, err)
	// The role is test-owned. Preserve the explicitly supplied password for
	// dedicated targets that require password authentication.
	_, err = admin.Exec(ctx, "ALTER ROLE "+identifier+" PASSWORD '"+strings.ReplaceAll(cfg.Password, "'", "''")+"'")
	require.NoError(t, err)
	cfg.Username, cfg.ReadOnly = role, true
	db, err := harmonydb.NewFromConfig(cfg)
	require.NoError(t, err)
	t.Cleanup(db.ITestDeleteAll)
	var actual, isolation, address string
	require.NoError(t, db.QueryRow(ctx, `SELECT current_setting('TimeZone'),current_setting('transaction_isolation'),host(inet_server_addr())`).Scan(&actual, &isolation, &address))
	require.Equal(t, zone, actual)
	require.Equal(t, "127.0.0.1", address)
	t.Logf("pool=%s TimeZone=%s requested isolation=driver default; reported isolation=%s (not proof of Yugabyte effective isolation)", suffix, actual, isolation)
	return db
}

func TestRetrySQLTimeZoneMatrix(t *testing.T) {
	for _, pair := range [][2]string{{"UTC", "UTC"}, {"Asia/Seoul", "Asia/Seoul"}, {"UTC", "Asia/Seoul"}, {"Asia/Seoul", "UTC"}} {
		t.Run(strings.ReplaceAll(pair[0]+"_to_"+pair[1], "/", "-"), func(t *testing.T) {
			var cfg harmonydb.Config
			ctx, _, _, admin := attemptSQLFixtureSchema(t, true, func(c harmonydb.Config) { cfg = c })
			writer := retryZonePool(t, ctx, admin, cfg, "writer", pair[0])
			claimant := retryZonePool(t, ctx, admin, cfg, "claimant", pair[1])
			hs := make([]*taskTypeHandler, 2)
			for i, db := range []*harmonydb.DB{writer, claimant} {
				e := &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101 + i, hostAndPort: "fixture.example"}}
				h := &taskTypeHandler{TaskEngine: e, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), RetryWait: func(int) time.Duration { return time.Hour }}}
				e.handlers, hs[i] = []*taskTypeHandler{h}, h
			}
			_, err := writer.Exec(ctx, `INSERT INTO harmony_task(id,name,added_by,posted_time,update_time,retries) VALUES(1,'PoRep',101,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)`)
			require.NoError(t, err)
			poll := func(h *taskTypeHandler) task {
				t.Helper()
				rows := h.TaskEngine.pollAllTaskTypes()["PoRep"]
				require.Len(t, rows, 1)
				var instant time.Time
				require.NoError(t, writer.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=1`).Scan(&instant))
				assert.True(t, instant.Equal(rows[0].UpdateTime), "poll shifted TIMESTAMPTZ instant: stored=%s polled=%s", instant, rows[0].UpdateTime)
				return rows[0]
			}
			row := poll(hs[1])
			ids, err := hs[1].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
			require.NoError(t, err)
			require.Empty(t, ids, "future positive retry must not claim")
			_, err = writer.Exec(ctx, `UPDATE harmony_task SET update_time=CURRENT_TIMESTAMP-INTERVAL '2 hours' WHERE id=1`)
			require.NoError(t, err)
			row = poll(hs[0])
			wire, err := json.Marshal(PeerMessage{Verb: string(messageTypeNewTask), TaskID: row.ID, Other: taskOther{TaskType: "PoRep", Retries: row.Retries, Posted: row.PostedTime, UpdateTime: row.UpdateTime}})
			require.NoError(t, err)
			ch := make(chan schedulerEvent, 1)
			p := &peering{h: &TaskEngine{schedulerChannel: ch}}
			require.NoError(t, p.handlePeerMessage("fixture.example", 1, wire))
			peer := taskFromSchedulerEvent(<-ch)
			require.True(t, peer.UpdateTime.Equal(row.UpdateTime))
			ids, err = hs[1].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, peer)
			require.NoError(t, err)
			require.Equal(t, []TaskID{1}, ids, "due peer snapshot must claim across session zones")
			ids, err = hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
			require.NoError(t, err)
			require.Empty(t, ids, "second owner must not overwrite the winner")
			retry := hs[1].recordCompletion(1, nil, time.Now(), false, errors.New("synthetic sector failure"), false)
			require.NotNil(t, retry)
			actual := poll(hs[0])
			require.True(t, retry.UpdateTime.Equal(actual.UpdateTime), "completion RETURNING shifted the instant")
			require.Equal(t, 2, retry.Retries)
			ids, err = hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
			require.NoError(t, err)
			require.Empty(t, ids, "stale retry must not claim")
			ids, err = hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, *retry)
			require.NoError(t, err)
			require.Empty(t, ids, "ordinary failure still has backoff")
			_, err = writer.Exec(ctx, `UPDATE harmony_task SET update_time=CURRENT_TIMESTAMP-INTERVAL '2 hours' WHERE id=1`)
			require.NoError(t, err)
			row = poll(hs[0])
			ids, err = hs[0].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, row)
			require.NoError(t, err)
			require.Equal(t, []TaskID{1}, ids)
			prepareRetryFixtureAttempt(t, ctx, writer, 101, 1, "preempt-token")
			retry = hs[0].recordCompletion(1, nil, time.Now(), false, context.Canceled, true, "preempt-token")
			require.NotNil(t, retry)
			require.True(t, retry.UpdateTime.Equal(row.UpdateTime), "preemption must retain the already-satisfied instant")
			ids, err = hs[1].claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, *retry)
			require.NoError(t, err)
			require.Equal(t, []TaskID{1}, ids)
			prepareRetryFixtureAttempt(t, ctx, claimant, 102, 1, "probe-token")
			require.Nil(t, hs[0].recordCompletion(1, nil, time.Now(), false, context.Canceled, true, "preempt-token"), "stale preemption cannot release a newer attempt")

		})
	}
}

func TestRetrySQLPositivePreemptionImmediatelyReclaims(t *testing.T) {
	ctx, db, other := retrySQLDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,name,added_by,posted_time,update_time,retries,owner_id)
 VALUES(1,'PoRep',101,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP-INTERVAL '2 hours',1,101)`)
	require.NoError(t, err)
	h := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101}}, TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(1), RetryWait: func(int) time.Duration { return time.Hour }}}
	prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "preempted-attempt")
	retry := h.recordCompletion(1, nil, time.Now(), false, context.Canceled, true, "preempted-attempt")
	require.NotNil(t, retry)
	require.Equal(t, 1, retry.Retries, "preemption neither consumes nor resets sector failures")
	ch := make(chan schedulerEvent, 1)
	h.emitRetryTask(eventEmitter{schedulerChannel: ch}, retry)
	select {
	case event := <-ch:
		require.True(t, retryReady(taskFromSchedulerEvent(event), h.RetryWait, time.Now()))
	case <-time.After(200 * time.Millisecond):
		t.Fatal("positive-retry preemption must re-emit immediately, not wait another hour")
	}
	claimant := &taskTypeHandler{TaskEngine: &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: other, ownerID: 102}}, TaskTypeDetails: h.TaskTypeDetails}
	ids, err := claimant.claimTaskOwnership([]TaskID{1}, 1, map[TaskID]int64{}, *retry)
	require.NoError(t, err)
	require.Equal(t, []TaskID{1}, ids, "immediate notification must also be eligible in authoritative SQL")
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=1 AND err='preempted' AND NOT result`).Scan(&count))
	require.Equal(t, 1, count)
}

func prepareRetryFixtureAttempt(t *testing.T, ctx context.Context, db *harmonydb.DB, owner int, id TaskID, token string) {
	t.Helper()
	var generation int64
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_generation FROM harmony_task WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&generation))
	store := harmonyTaskAttemptStore{db: db, owner: owner, generations: map[TaskID]int64{id: generation}, token: token}
	require.NoError(t, store.prepare(ctx, id, token))
}

func retrySQLDB(t *testing.T) (context.Context, *harmonydb.DB, *harmonydb.DB) {
	ctx, db, other, _ := attemptSQLFixture(t)
	return ctx, db, other
}
