//go:build integration && !skiff

package harmonytask

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func porepLifecycleDB(t *testing.T, capture ...func(harmonydb.Config)) (context.Context, *harmonydb.DB, *harmonydb.DB) {
	t.Helper()
	ctx, first, second, _ := attemptSQLFixtureSchema(t, true, capture...)
	return ctx, first, second
}

func TestPoRepLifecycleSQLTerminalFailureRetainsEvidence(t *testing.T) {
	ctx, db, _ := porepLifecycleDB(t)
	_, err := db.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,added_by,name,owner_id)
 VALUES(1,CURRENT_TIMESTAMP,101,'PoRep',101)`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline(sp_id,sector_number,reg_seal_proof,task_id_porep,after_tree_r,after_precommit_msg_success,seed_epoch)
 VALUES(1000,1,0,1,TRUE,TRUE,1)`)
	require.NoError(t, err)
	h := &taskTypeHandler{TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(2), MaxFailures: 10},
		TaskEngine: &TaskEngine{cfg: taskEngineConfig{db: db, ownerID: 101, hostAndPort: "worker-a.example"}}}
	for attempt := 1; attempt <= 10; attempt++ {
		identity := prepareRetryFixtureAttempt(t, ctx, db, 101, 1, "fixture-attempt")
		h.recordCompletion(1, &abi.SectorID{Miner: 1000, Number: 1}, time.Now(), false, errors.New("synthetic proof failure"), false, identity)
		var rows int
		require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task WHERE id=1`).Scan(&rows))
		if attempt < 10 {
			require.Equal(t, 1, rows)
			var retries int
			var unowned bool
			require.NoError(t, db.QueryRow(ctx, `SELECT retries,owner_id IS NULL FROM harmony_task WHERE id=1`).Scan(&retries, &unowned))
			require.Equal(t, attempt, retries)
			require.True(t, unowned)
			_, err = db.Exec(ctx, `UPDATE harmony_task SET owner_id=101 WHERE id=1`)
			require.NoError(t, err)
		} else {
			require.Zero(t, rows)
		}
	}
	var ref int64
	var failed, after, finalized, moved bool
	require.NoError(t, db.QueryRow(ctx, `SELECT task_id_porep,failed,after_porep,after_finalize,after_move_storage
 FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=1`).Scan(&ref, &failed, &after, &finalized, &moved))
	require.EqualValues(t, 1, ref)
	require.False(t, failed)
	require.False(t, after)
	require.False(t, finalized)
	require.False(t, moved)
	var history, events int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history WHERE task_id=1 AND result=FALSE`).Scan(&history))
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sectors_pipeline_events WHERE sp_id=1000 AND sector_number=1`).Scan(&events))
	require.Equal(t, 10, history)
	require.Equal(t, 10, events)
}

type porepHeldEntry struct {
	id  TaskID
	ctx context.Context
	end chan bool
}

type porepHeldTask struct {
	stubAcceptTask
	entries chan porepHeldEntry
	stop    chan struct{}
}

func (p *porepHeldTask) Do(ctx context.Context, id TaskID, _ func() bool) (bool, error) {
	e := porepHeldEntry{id: id, ctx: ctx, end: make(chan bool, 1)}
	p.entries <- e
	// Deliberately ignore ctx until the test releases the native substitute.
	// A cancelled Go context must not refund an occupied native slot.
	select {
	case success := <-e.end:
		if success {
			return true, nil
		}
		return false, errors.New("synthetic native return")
	case <-p.stop:
		return false, errors.New("fixture shutdown")
	}
}

func TestPoRepLifecycleSQLFailuresLateReturnAndRefill(t *testing.T) {
	ctx, first, second := porepLifecycleDB(t)
	for id := 1; id <= 16; id++ {
		_, err := first.Exec(ctx, `INSERT INTO harmony_task(id,posted_time,added_by,name,retries)
 VALUES($1,CURRENT_TIMESTAMP,101,'PoRep',9)`, id)
		require.NoError(t, err)
	}
	// Independent handles/instances; SQL ownership and completion are real.
	// CanAccept and Do are substitutes; no chain, GPU or proof code runs.
	var handlers []*taskTypeHandler
	var heldTasks []*porepHeldTask
	var emitters []eventEmitter
	var stopOnce sync.Once
	stop := make(chan struct{})
	for i, db := range []*harmonydb.DB{first, second} {
		p := &porepHeldTask{entries: make(chan porepHeldEntry, 4), stop: stop}
		e := &TaskEngine{cfg: taskEngineConfig{ctx: ctx, db: db, ownerID: 101 + i, hostAndPort: "fixture.example",
			reg: &resources.Reg{Resources: resources.Resources{Cpu: 4, Gpu: 2, Ram: 1 << 20}}}, admissionWake: make(chan struct{}, 1)}
		h := &taskTypeHandler{TaskInterface: p, TaskEngine: e,
			TaskTypeDetails: TaskTypeDetails{Name: "PoRep", Max: taskhelp.Max(2), MaxFailures: 10, Cost: resources.Resources{Cpu: 1, Gpu: 1}},
			running:         runregistry.New(), accept: acceptcache.New(time.Hour), storageFailures: map[TaskID]time.Time{}}
		e.handlers = []*taskTypeHandler{h}
		handlers = append(handlers, h)
		heldTasks = append(heldTasks, p)
		emitters = append(emitters, eventEmitter{schedulerChannel: make(chan schedulerEvent, 100)})
	}
	// Release every native substitute before waiting; never wait while holding
	// its return barrier. Wait for completion events before fixture DB cleanup.
	started, completed := []int{0, 0}, []int{0, 0}
	waitCompletion := func(i int) {
		for {
			select {
			case event := <-emitters[i].schedulerChannel:
				if event.Source == schedulerSourceTaskCompleted {
					completed[i]++
					return
				}
			case <-ctx.Done():
				t.Fatal("completion persistence/event did not return")
			case <-handlers[i].TaskEngine.admissionWake:
				handlers[i].drainAdmissions()
			}
		}
	}
	t.Cleanup(func() {
		stopOnce.Do(func() { close(stop) })
		for i := range handlers {
			for completed[i] < started[i] {
				waitCompletion(i)
			}
		}
	})
	for wave := 0; wave < 4; wave++ {
		var entries [2][]porepHeldEntry
		for i, h := range handlers {
			ids := []task{}
			// Both instances see the same advisory list. The second must skip
			// the first's committed claim, not dispatch it a second time.
			for id := wave*4 + 1; id <= wave*4+4; id++ {
				var updated time.Time
				require.NoError(t, first.QueryRow(ctx, `SELECT update_time FROM harmony_task WHERE id=$1`, id).Scan(&updated))
				ids = append(ids, task{ID: TaskID(id), Retries: 9, UpdateTime: updated})
			}
			require.True(t, h.considerWork(workSourcePoller, ids, emitters[i]))
			started[i] += 2
			for j := 0; j < 2; j++ {
				select {
				case e := <-heldTasks[i].entries:
					entries[i] = append(entries[i], e)
				case <-ctx.Done():
					t.Fatal("Do not entered")
				case <-h.TaskEngine.admissionWake:
					h.drainAdmissions()
					j--
				}
			}
			require.Equal(t, 2, h.Max.Active())
			require.Len(t, h.running.Snapshot(), 2)
			// Cancellation acknowledged inside the native substitute is not
			// native completion. Neither capacity nor ownership is released yet.
			handle, ok := h.running.Get(int64(entries[i][0].id))
			require.True(t, ok)
			handle.Preempt()
			<-entries[i][0].ctx.Done()
			require.Equal(t, 2, h.Max.Active())
			require.False(t, handle.WaitDone(time.Now()))
			var owner int
			require.NoError(t, first.QueryRow(ctx, `SELECT owner_id FROM harmony_task WHERE id=$1`, entries[i][0].id).Scan(&owner))
			require.Equal(t, 101+i, owner)
			_ = h.considerWork(workSourcePoller, ids, emitters[i])
			select {
			case e := <-heldTasks[i].entries:
				t.Fatalf("double dispatch: %d", e.id)
			default:
			}
		}
		require.NotEqual(t, entries[0][0].id, entries[1][0].id)
		for i, h := range handlers {
			entries[i][0].end <- false
			entries[i][1].end <- wave%2 == 0
			waitCompletion(i)
			waitCompletion(i)
			require.Zero(t, h.Max.Active())
			require.Empty(t, h.running.Snapshot())
		}
	}
	var history int
	require.NoError(t, first.QueryRow(ctx, `SELECT count(*) FROM harmony_task_history`).Scan(&history))
	require.Equal(t, 16, history)
}
