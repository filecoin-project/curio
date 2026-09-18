package harmonytask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/acceptcache"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

var log = logging.Logger("harmonytask")

// pipelineTask lets a task expose the sector it relates to so harmony can
// surface sector context in task history. It is an unexported, structural
// interface: external implementations (e.g. sealing tasks) satisfy it by
// having a matching GetSectorID method, without needing to reference this
// type by name.
type pipelineTask interface {
	GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error)
}

// Context-aware implementations can bound/cancel diagnostic lookup before Do.
// Legacy pipeline tasks retain their existing interface.
type contextualPipelineTask interface {
	GetSectorIDContext(context.Context, *harmonydb.DB, int64) (*abi.SectorID, error)
}

// taskTypeHandler wraps a TaskInterface with scheduling metadata and runtime
// state for its task type. The fields fall into three disjoint access
// regimes — each one clearly labeled below — so a reader can tell at a
// glance how a given field is safe to touch.
type taskTypeHandler struct {
	// --- immutable after New ---
	TaskInterface
	TaskTypeDetails
	TaskEngine *TaskEngine

	// --- scheduler-goroutine only (no locking, single-threaded invariant) ---
	//
	// storageFailures is only read/written from considerWork, which runs on
	// the scheduler thread. The single-writer invariant is what makes this
	// safe without a mutex.
	storageFailures      map[TaskID]time.Time
	lastAcceptRefusalLog time.Time

	// --- concurrent state, encapsulated behind typed APIs ---
	//
	// The mutex and backing store for each of these lives inside an internal
	// sub-package, so nothing in this package can reach them without going
	// through methods that lock correctly.
	running            *runregistry.Registry
	accept             *acceptcache.Cache
	admissions         map[TaskID]*taskAdmission
	completionRecorder func(TaskID, bool, error)
	admissionFactory   func(string, []task) (func([]TaskID, int) ([]TaskID, error), taskAttemptStore)
}

// canAcceptCacheTTL controls how long pre-computed CanAccept results remain
// valid. After this duration, the cache is discarded and CanAccept is called
// fresh. This balances DB load reduction against decision freshness.
const canAcceptCacheTTL = 60 * time.Second

// storageFailureTimeout prevents repeatedly trying to claim storage for a
// task that just failed storage allocation. The task is retried after this
// cooldown or on the next process restart.
const storageFailureTimeout = 3 * time.Minute

// workSource* identify how work was discovered. They are passed into
// considerWork as the "from" argument (stats, logging, recover path).
const (
	workSourcePoller   = "poller"
	workSourceRecover  = "recovered"
	workSourcePreempt  = "preempt"
	workSourceOverride = "scheduling-override"
)

// considerWork is the core scheduling function for a single task type. It
// runs on the scheduler thread and is the only code path that starts task
// goroutines. The function follows a strict pipeline:
//
//  1. Check concurrency limit (Max) — are we at capacity for this type?
//  2. Check machine resources (CPU/RAM/GPU) — can we fit another task?
//  3. CanAccept filter — either from cache (background poller) or fresh call.
//     This is the task-specific decision (e.g., "do I handle this miner?").
//  4. SQL claim — UPDATE SET owner_id with SKIP LOCKED to atomically claim
//     tasks. SKIP LOCKED ensures exactly one winner when multiple nodes race.
//  5. Storage claim — for tasks with disk requirements, claim storage paths.
//     This is late-bound (after SQL claim) because different tasks may use
//     different paths that can't be predicted before the specific task ID is known.
//  6. Launch goroutine — each claimed task runs in its own goroutine, emitting
//     TaskStarted and TaskCompleted events back to the scheduler.
//
// Single-threaded invariant: this function must only be called from the
// scheduler goroutine. Task goroutines that complete may change resource
// availability, but that's safe — resources can only increase, never
// invalidating a "fits" decision made moments earlier.
func (h *taskTypeHandler) considerWork(from string, tasks []task, eventEmitter eventEmitter) (workAccepted bool) {
	generations := make(map[TaskID]int64)
	store := taskAttemptStore(harmonyTaskAttemptStore{db: h.TaskEngine.cfg.db, owner: h.TaskEngine.cfg.ownerID, generations: generations})
	release := h.releaseTaskOwnership
	claim := func(ids []TaskID, limit int) ([]TaskID, error) {
		if from == workSourceRecover {
			return h.recoverTaskOwnership(tasks, ids, limit, generations)
		}
		return h.claimTaskOwnership(ids, limit, generations, tasks...)
	}
	if h.admissionFactory != nil {
		claim, store = h.admissionFactory(from, tasks)
		release = nil
	}
	return h.considerWorkWithOwnership(from, tasks, eventEmitter, claim, release, store)
}

// The ownership callbacks keep failure paths testable without changing the
// production SQL or requiring a live database for admission lifecycle tests.
func (h *taskTypeHandler) considerWorkWithOwnership(from string, tasks []task, eventEmitter eventEmitter,
	claim func([]TaskID, int) ([]TaskID, error), release func([]TaskID, map[TaskID]string) error, attemptStores ...taskAttemptStore) (workAccepted bool) {
	var attemptStore taskAttemptStore
	if len(attemptStores) > 0 {
		attemptStore = attemptStores[0]
	}
	if len(tasks) == 0 {
		return true
	}

	// Skip IDs already running on this node. After a successful claim,
	// running.Start happens before considerWork returns, so a later waterfall
	// (TaskCompleted / bundle / tryStart) must not re-claim and log "already Taken".
	{
		tasks = lo.Filter(tasks, func(t task, _ int) bool {
			_, running := h.running.Get(int64(t.ID))
			return !running && h.admissions[t.ID] == nil
		})
		if len(tasks) == 0 {
			return true
		}
	}

	if h.TaskEngine.cfg.ctx.Err() != nil || h.TaskEngine.atomics.draining.Load() {
		return false
	}
	if h.admissions == nil {
		h.admissions = make(map[TaskID]*taskAdmission)
	}
	if len(h.admissions) >= maxPendingAdmissions {
		return false
	}
	if h.Max.AtMax() {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at max already")
		return false
	}

	maxAcceptable, err := h.AssertMachineHasCapacity()
	if err != nil {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at capacity already: "+err.Error())
		return false
	}
	if gate, ok := h.TaskInterface.(taskStartReadiness); ok && gate.TaskStartBlocked() {
		return false
	}

	ids := lo.Map(tasks, func(t task, _ int) TaskID {
		return t.ID
	})
	tIDs, err := h.resolveAcceptedIDs(ids)
	if err != nil {
		log.Error(err)
		return false
	}
	if len(tIDs) == 0 {
		if h.shouldLogAcceptRefusal(time.Now()) {
			log.Infow("did not accept task", "candidate_count", len(ids), "task_id_sample", ids[:min(5, len(ids))], "reason", "CanAccept() refused", "name", h.Name)
		}
		return false
	}

	tIDs = reorderTaskIDsByPostedOrder(tasks, tIDs)

	maxAcceptable = min(maxAcceptable, maxPendingAdmissions-len(h.admissions))
	headroomUntilMax := h.Max.Headroom()
	if maxAcceptable > headroomUntilMax {
		maxAcceptable = headroomUntilMax
	}

	tIDs = lo.Filter(tIDs, func(tID TaskID, _ int) bool {
		v, ok := h.storageFailures[tID]
		if !ok {
			return true
		}
		if time.Since(v) > storageFailureTimeout {
			delete(h.storageFailures, tID)
			return true
		}
		return false
	})
	tIDs = reorderTaskIDsByPostedOrder(tasks, tIDs)

	hadCandidates := len(tIDs) != 0
	tIDs, startReservation := reserveTaskStart(h.TaskInterface, tIDs)
	if hadCandidates && len(tIDs) == 0 {
		return false
	}
	dispatched := false
	defer func() {
		if startReservation != nil && !dispatched {
			startReservation.cancel()
		}
	}()

	{
		tasksAccepted, err := claim(tIDs, maxAcceptable)
		if err != nil {
			log.Error(err)
			return false
		}
		if len(tasksAccepted) == 0 {
			// Multi-node contention (or a rare local race). Self-races after a
			// successful claim are filtered via running / NoteClaimed.
			log.Debugw("did not accept task", "task_id", tIDs, "reason", "already Taken", "name", h.Name)

			return false
		}
		if len(tasksAccepted) > maxAcceptable {
			panic("claim exceeded admission headroom")
		}
		if len(tasksAccepted) != len(tIDs) {
			remainder := lo.Filter(tIDs, func(tID TaskID, _ int) bool {
				return !lo.Contains(tasksAccepted, tID)
			})
			h.accept.Add(toInt64s(remainder))
			tIDs = tasksAccepted
		}
	}

	if attemptStore == nil {
		attemptStore = harmonyTaskAttemptStore{db: h.TaskEngine.cfg.db, owner: int(h.TaskEngine.cfg.ownerID)}
	}
	eventEmitter.NoteClaimed(h.Name, tIDs)
	for _, id := range tIDs {
		h.beginAdmission(from, id, tasks, attemptStore, startReservation, eventEmitter, release)
	}
	dispatched = true // accepted includes pending, not just Do-entered
	return true
}

func (h *taskTypeHandler) dispatchAdmission(a *taskAdmission) {
	tID, from, tasks, attemptStore, eventEmitter, handle := a.id, a.from, a.tasks, a.store, a.ee, a.handle
	identity := completionIdentityFor(attemptStore, tID, a.token)
	taskCancel := a.cancel
	meta := &completionMeta{vals: make(map[any]any)}
	taskCtx := context.WithValue(a.ctx, completionMetaKey{}, meta)
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(taskNameTag, h.Name), tag.Upsert(sourceTag, from)}, TaskMeasures.TasksStarted.M(1))
	go func() {
		var done bool
		var doErr error
		workStart := handle.StartTime()
		var sectorID *abi.SectorID

		// Install cleanup before diagnostic lookup or event emission. Neither
		// is allowed to leave Max/running/storage or an unstarted reservation
		// stranded on panic. Completion persistence may itself take time.
		defer func() {
			if r := recover(); r != nil {
				stackSlice := make([]byte, 4092)
				sz := runtime.Stack(stackSlice, false)
				log.Error("Recovered from a serious error "+
					"while processing "+h.Name+" task "+strconv.Itoa(int(tID))+": ", r,
					" Stack: ", string(stackSlice[:sz]))
			}

			select {
			case <-a.entered:
			default:
				a.handle.CancelPending()
				return
			}
			taskCancel()

			preempted := handle.IsPreempted()
			a.releaseLocal()
			var result taskCompletion
			if h.completionRecorder != nil {
				h.completionRecorder(tID, done, doErr)
				result.applied = true // Test-only recorder replaces persistence.
			} else {
				result = h.recordCompletion(tID, sectorID, workStart, done, doErr, preempted, identity)
			}
			h.publishCompletion(result, tID, meta, done, doErr, eventEmitter)
		}()

		defer taskCancel()
		for _, snapshot := range tasks {
			if snapshot.ID == tID {
				eventEmitter.EmitTaskStarted(h.Name, tID, snapshot)
				break
			}
		}
		// This value is diagnostic only; Do retains its authoritative lookup.
		// Keep errors local to the goroutine, including concurrent batch tasks.
		var sectorErr error
		switch ht := h.TaskInterface.(type) {
		case contextualPipelineTask:
			sectorID, sectorErr = ht.GetSectorIDContext(taskCtx, h.TaskEngine.cfg.db, int64(tID))
		case pipelineTask:
			sectorID, sectorErr = ht.GetSectorID(h.TaskEngine.cfg.db, int64(tID))
		}
		if sectorErr != nil {
			log.Errorw("Could not get sector ID", "task", h.Name, "id", tID, "error", sectorErr)
		}
		log.Infow("Beginning work on Task", "id", tID, "from", from, "name", h.Name, "sector", sectorID)

		beforeStart := a.enter
		done, doErr = runWithAttemptStart(taskCtx, attemptStore, tID, a.token, beforeStart, time.Now, func(start time.Time) {
			workStart = start
		}, func() (bool, error) {
			return h.Do(taskCtx, tID, func() bool {
				if taskCtx.Err() != nil {
					return false
				}
				if taskhelp.IsBackgroundTask(h.Name) || h.CanYield {
					if h.TaskEngine.atomics.yieldBackground.Load() {
						log.Infow("yielding background task", "name", h.Name, "id", tID)
						return false
					}
				}
				// Uninterruptible work (e.g. Send*) calls stillOwned before
				// taking a per-sender lock. During shutdown drain, fail that
				// check so hundreds of waiters can exit without starting a new
				// critical section; in-flight holders do not call stillOwned
				// and are waited on via Active() in GracefullyTerminate.
				if h.Uninterruptible && h.TaskEngine.atomics.draining.Load() {
					log.Infow("yielding uninterruptible task during shutdown drain", "name", h.Name, "id", tID)
					return false
				}

				var owner int
				err := h.TaskEngine.cfg.db.QueryRow(taskCtx,
					`SELECT owner_id FROM harmony_task WHERE id=$1`, tID).Scan(&owner)
				if err != nil {
					log.Error("Cannot determine ownership: ", err)
					return false
				}
				return owner == h.TaskEngine.cfg.ownerID
			})
		})
		if doErr != nil {
			log.Errorw("Do() returned error", "type", h.Name, "id", strconv.Itoa(int(tID)), "error", doErr)
		}
	}()
}

func (h *taskTypeHandler) claimTaskOwnership(ids []TaskID, maxAcceptable int, generations map[TaskID]int64, observed ...task) ([]TaskID, error) {
	expected := make(map[TaskID]task, len(observed))
	for _, t := range observed {
		expected[t.ID] = t
	}
	retries := make([]int, len(ids))
	updates := make([]*time.Time, len(ids))
	waits := make([]int64, len(ids))
	for i, id := range ids {
		t, ok := expected[id]
		if !ok {
			return nil, fmt.Errorf("missing claim snapshot for task %d", id)
		}
		retries[i] = t.Retries
		// First notifications do not carry an authoritative retry timestamp.
		if t.Retries > 0 && !t.UpdateTime.IsZero() {
			updates[i] = &t.UpdateTime
		}
		if t.Retries > 0 && h.RetryWait != nil {
			waits[i] = h.RetryWait(t.Retries).Microseconds()
		}
	}
	var accepted []struct {
		ID              TaskID
		OwnerGeneration int64 `db:"owner_generation"`
	}
	err := h.TaskEngine.cfg.db.Select(h.TaskEngine.cfg.ctx, &accepted, claimTaskOwnershipSQL,
		h.TaskEngine.cfg.ownerID, ids, maxAcceptable, retries, updates, waits)
	idsOut := make([]TaskID, 0, len(accepted))
	for _, row := range accepted {
		idsOut = append(idsOut, row.ID)
		generations[row.ID] = row.OwnerGeneration
	}
	return idsOut, err
}

const claimTaskOwnershipSQL = `
		WITH expected AS (
			SELECT * FROM unnest($2::bigint[], $4::int[], $5::timestamptz[], $6::bigint[])
			AS x(id, retries, updated, wait_us)
		), candidates AS (
			SELECT t.id, x.retries, x.updated, x.wait_us
			FROM harmony_task t
			JOIN expected x ON x.id = t.id
			WHERE t.owner_id IS NULL AND t.retries = x.retries
			  AND (t.update_time = x.updated OR (x.updated IS NULL AND x.retries = 0))
			  AND (t.retries = 0 OR CURRENT_TIMESTAMP >= t.update_time + x.wait_us * INTERVAL '1 microsecond')
			ORDER BY array_position($2, t.id::bigint)
			LIMIT $3
			FOR UPDATE OF t SKIP LOCKED
		)
		UPDATE harmony_task t
		SET owner_id = $1
		FROM candidates c
		WHERE t.id = c.id AND t.owner_id IS NULL AND t.retries = c.retries
		  AND (t.update_time = c.updated OR (c.updated IS NULL AND c.retries = 0))
		  AND (t.retries = 0 OR CURRENT_TIMESTAMP >= t.update_time + c.wait_us * INTERVAL '1 microsecond')
		RETURNING t.id, t.owner_generation;`

const releasePreparedTaskOwnershipSQL = `UPDATE harmony_task AS t
SET owner_id = NULL
FROM unnest($1::bigint[], $2::text[]) AS failed(id, attempt_id)
WHERE t.id = failed.id AND t.owner_id = $3
  AND t.attempt_id = failed.attempt_id
  AND t.attempt_started_at IS NULL AND t.attempt_start_source = 'prepared'`

func (h *taskTypeHandler) releaseTaskOwnership(ids []TaskID, tokens map[TaskID]string) error {
	return releasePreparedTaskOwnership(ids, tokens, int(h.TaskEngine.cfg.ownerID),
		func(ctx context.Context, ids []int64, attempts []string, owner int) (int, error) {
			return h.TaskEngine.cfg.db.Exec(ctx, releasePreparedTaskOwnershipSQL, ids, attempts, owner)
		})
}

// releasePreparedTaskOwnership is only for failed storage claims after attempt
// preparation. Match the attempt as well as the owner: the same machine may
// have acquired a newer attempt by the time this cleanup reaches the DB.
// One batch and one independent cleanup budget cover every failed ID.
func releasePreparedTaskOwnership(ids []TaskID, tokens map[TaskID]string, owner int,
	exec func(context.Context, []int64, []string, int) (int, error)) error {
	if len(ids) == 0 {
		return nil
	}
	attempts := make([]string, len(ids))
	for i, id := range ids {
		token := tokens[id]
		if token == "" {
			return fmt.Errorf("cannot release task %d without its prepared attempt token", id)
		}
		attempts[i] = token
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := exec(ctx, toInt64s(ids), attempts, owner)
	return err
}

// emitRetryTask re-adds a failed or preempted task to the scheduler after
// recordCompletion. Failed tasks sleep RetryWait before emitting so the task is
// not visible to scheduling (or peers) until the backoff elapses. Tasks dropped
// after MaxFailures are not re-emitted.
func (h *taskTypeHandler) emitRetryTask(eventEmitter eventEmitter, retry *task) {
	if retry == nil {
		return
	}
	ctx := h.TaskEngine.cfg.ctx
	wait := max(0, time.Until(retryDeadline(*retry, h.RetryWait)))
	go func() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		select {
		case <-ctx.Done():
		case eventEmitter.schedulerChannel <- schedulerEvent{TaskID: retry.ID, TaskType: h.Name, Source: schedulerSourceAdded, Retries: retry.Retries, PostedTime: retry.PostedTime, UpdateTime: retry.UpdateTime}:
		}
	}()
}

// recordCompletion persists the task outcome to the DB. This MUST complete
// even during graceful shutdown (uses context.Background), because an
// incomplete record would leave the task in a claimed-but-not-running state.
//
// On success: DELETE from harmony_task, INSERT into harmony_task_history.
// On failure: either retry (UPDATE retries++, SET owner_id=NULL) or
// permanently drop (DELETE) if MaxFailures is exceeded.
//
// Retries with exponential backoff on DB errors to guarantee eventual
// persistence. If the process restarts before completion, the resurrection
// logic in New() will recover the task.
func (h *taskTypeHandler) recordCompletion(tID TaskID, sectorID *abi.SectorID, workStart time.Time, done bool, doErr error, preempted bool, identity completionIdentity) taskCompletion {
	workEnd := time.Now()
	retryWait := time.Millisecond * 100
	if !identity.valid {
		log.Errorw("Ignoring completion without acquisition identity", "task", h.Name, "id", tID)
		return taskCompletion{}
	}

	{
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(taskNameTag, h.Name),
		}, TaskMeasures.ActiveTasks.M(int64(h.Max.ActiveThis())))

		duration := workEnd.Sub(workStart).Seconds()
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(taskNameTag, h.Name),
		}, TaskMeasures.TaskDuration.M(duration))

	}

	var waitStartTime time.Time
	var retry *task
	var applied bool
retryRecordCompletion:
	cm, err := h.TaskEngine.cfg.db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		retry = nil // Results belong only to the committed transaction attempt.
		applied = false
		var postedTime time.Time
		var retries uint
		var updateTime time.Time
		err := tx.QueryRow(`SELECT posted_time, update_time, retries FROM harmony_task
 WHERE id=$1 AND owner_id=$2 AND owner_generation=$3 AND attempt_id=$4 FOR UPDATE`,
			tID, identity.owner, identity.generation, identity.token).Scan(&postedTime, &updateTime, &retries)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // Missing/superseded is a terminal no-op, not a DB outage.
		}
		if err != nil {
			return false, fmt.Errorf("could not log completion: %w ", err)
		}
		waitStartTime = postedTime
		if retries > 0 {
			waitStartTime = updateTime
		}
		result := ""
		var changed int
		switch {
		case done:
			changed, err = tx.Exec(`DELETE FROM harmony_task
 WHERE id=$1 AND owner_id=$2 AND owner_generation=$3 AND attempt_id=$4 AND retries=$5`,
				tID, identity.owner, identity.generation, identity.token, retries)
			if err != nil {
				return false, fmt.Errorf("could not log completion: %w", err)
			}
			result = ""
			if doErr != nil {
				result = "non-failing error: " + doErr.Error()
			}
		case preempted:
			retry = &task{ID: tID, Retries: int(retries), PostedTime: postedTime}
			// Preemption is not a new failure: retain the retry clock whose wait
			// was already satisfied at claim, for both peer and SQL eligibility.
			err = tx.QueryRow(`UPDATE harmony_task SET owner_id=NULL
 WHERE id=$1 AND owner_id=$2 AND owner_generation=$3 AND attempt_id=$4 AND retries=$5
 RETURNING update_time`, tID, identity.owner, identity.generation, identity.token, retries).Scan(&retry.UpdateTime)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("could not release preempted task: %v %v", tID, err)
			}
			result = "preempted"
			changed = 1
		default:
			result = "unspecified error"
			if doErr != nil {
				result = "error: " + doErr.Error()
			}
			var deleteTask bool
			if h.MaxFailures > 0 && retries >= h.MaxFailures-1 {
				deleteTask = true
			}
			if deleteTask {
				changed, err = tx.Exec(`DELETE FROM harmony_task
 WHERE id=$1 AND owner_id=$2 AND owner_generation=$3 AND attempt_id=$4 AND retries=$5`,
					tID, identity.owner, identity.generation, identity.token, retries)
				if err != nil {
					return false, fmt.Errorf("could not delete failed job: %w", err)
				}
			} else {
				retry = &task{ID: tID, Retries: int(retries) + 1, PostedTime: postedTime}
				err = tx.QueryRow(`UPDATE harmony_task SET owner_id=NULL, retries=retries+1, update_time=CURRENT_TIMESTAMP
 WHERE id=$1 AND owner_id=$2 AND owner_generation=$3 AND attempt_id=$4 AND retries=$5
 RETURNING update_time`, tID, identity.owner, identity.generation, identity.token, retries).Scan(&retry.UpdateTime)
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				if err != nil {
					return false, fmt.Errorf("could not disown failed task: %v %v", tID, err)
				}
				changed = 1
			}
		}
		if changed != 1 {
			return false, nil
		}

		var hid int
		err = tx.QueryRow(`INSERT INTO harmony_task_history 
									 (task_id, name, posted, work_start, work_end, result, completed_by_host_and_port, err)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, tID, h.Name, postedTime.UTC(), workStart.UTC(), workEnd.UTC(), done, h.TaskEngine.cfg.hostAndPort, result).Scan(&hid)
		if err != nil {
			return false, fmt.Errorf("could not write history: %w", err)
		}
		if sectorID != nil {
			_, err = tx.Exec(`SELECT append_sector_pipeline_events($1, $2, $3)`, uint64(sectorID.Miner), uint64(sectorID.Number), hid)
			if err != nil {
				return false, fmt.Errorf("could not append sector pipeline events: %w", err)
			}
		}

		applied = true
		return true, nil
	})
	if err != nil {
		time.Sleep(retryWait)
		retryWait *= 2
		if retryWait > time.Second*10 {
			log.Error("Could not record completion (retrying): ", err)
		}
		goto retryRecordCompletion
	}
	if !cm || !applied {
		log.Warnw("Ignored stale task completion; current task and history unchanged", "task", h.Name, "id", tID,
			"owner", identity.owner, "generation", identity.generation, "attempt", identity.token, "done", done, "error", doErr)
		return taskCompletion{}
	}
	if done {
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(taskNameTag, h.Name)}, TaskMeasures.TasksCompleted.M(1))
	} else if !preempted {
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(taskNameTag, h.Name)}, TaskMeasures.TasksFailed.M(1))
	}

	scheduledWait := workStart.Sub(waitStartTime).Seconds()
	if scheduledWait < 0 {
		scheduledWait = 0
	}
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, h.Name),
	}, TaskMeasures.TaskScheduledWait.M(scheduledWait))
	return taskCompletion{applied: true, retry: retry}
}

// maxHeadroom limits how many tasks of a single type can be accepted in one
// considerWork call. This prevents a single task type from monopolizing all
// resources. Configurable via HARMONY_MAX_TASKS_PER_TYPE env var.
var maxHeadroom = 100

func init() {
	m := os.Getenv("HARMONY_MAX_TASKS_PER_TYPE")
	if m == "" {
		return
	}
	v, err := strconv.Atoi(m)
	if err != nil {
		log.Errorw("Could not parse HARMONY_MAX_TASKS_PER_TYPE", "value", m, "error", err)
	}
	if v > 0 {
		maxHeadroom = v
	}
}

// AssertMachineHasCapacity checks whether this node has sufficient resources
// (CPU, RAM, GPU, storage) to run another task of this type. Returns the
// maximum number of additional tasks that could fit (headroom), constrained
// by the scarcest resource. Returns 0 with an error if no capacity exists.
func (h *taskTypeHandler) AssertMachineHasCapacity() (int, error) {
	r := h.TaskEngine.ResourcesAvailable()
	headroom := maxHeadroom
	if h.Max.AtMax() {
		return 0, errors.New("Did not accept " + h.Name + " task: at max already")
	}

	if r.Cpu-h.Cost.Cpu < 0 {
		return 0, xerrors.Errorf("Did not accept %s task: out of cpu: required %d available %d)", h.Name, h.Cost.Cpu, r.Cpu)
	}
	if h.Cost.Cpu > 0 {
		cpuHeadroom := r.Cpu / h.Cost.Cpu
		if cpuHeadroom < headroom {
			headroom = cpuHeadroom
		}
	}
	if h.Cost.Ram > r.Ram {
		return 0, xerrors.Errorf("Did not accept %s task: out of RAM: required %d available %d)", h.Name, h.Cost.Ram, r.Ram)
	}
	if h.Cost.Ram > 0 {
		ramHeadroom := r.Ram / h.Cost.Ram
		if ramHeadroom < uint64(headroom) {
			headroom = int(ramHeadroom)
		}
	}
	if r.Gpu-h.Cost.Gpu < 0 {
		return 0, xerrors.Errorf("Did not accept %s task: out of available GPU: required %f available %f)", h.Name, h.Cost.Gpu, r.Gpu)
	}
	if h.Cost.Gpu > 0 {
		gpuHeadroom := r.Gpu / h.Cost.Gpu
		if gpuHeadroom < float64(headroom) {
			headroom = int(gpuHeadroom)
		}
	}

	if h.Cost.Storage != nil {
		if !h.Cost.HasCapacity() {
			return 0, errors.New("Did not accept " + h.Name + " task: out of available Storage")
		}
	}
	return headroom, nil
}

// reorderTaskIDsByPostedOrder sorts accepted IDs by the order they appear in tasks (posted_time FIFO from the DB/poller).
func reorderTaskIDsByPostedOrder(tasks []task, ids []TaskID) []TaskID {
	if len(ids) <= 1 {
		return ids
	}
	want := make(map[TaskID]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]TaskID, 0, len(ids))
	for _, t := range tasks {
		if _, ok := want[t.ID]; ok {
			out = append(out, t.ID)
			delete(want, t.ID)
		}
	}
	for _, id := range ids {
		if _, ok := want[id]; ok {
			out = append(out, id)
			delete(want, id)
		}
	}
	return out
}

// resolveAcceptedIDs applies the acceptcache (when fresh) and falls back to a
// live CanAccept call on miss, expiry, or empty intersection. An empty
// intersection with a non-empty cache is a miss for this candidate set — not
// a CanAccept refusal.
func (h *taskTypeHandler) resolveAcceptedIDs(ids []TaskID) ([]TaskID, error) {
	if _, ok := h.TaskInterface.(taskCandidateFilter); ok {
		var err error
		ids, err = filterTaskCandidates(h.TaskEngine.cfg.ctx, h.TaskInterface, ids)
		if err != nil || len(ids) == 0 {
			return nil, err
		}
	}
	matched, hadFresh := h.accept.TakeMatching(toInt64s(ids))
	if hadFresh && len(matched) > 0 {
		tIDs := toTaskIDs(matched)
		missing := lo.Filter(ids, func(id TaskID, _ int) bool {
			return !lo.Contains(tIDs, id)
		})
		if len(missing) == 0 {
			return tIDs, nil
		}
		extra, err := h.CanAccept(missing, h.TaskEngine)
		if err != nil {
			return nil, err
		}
		return append(tIDs, extra...), nil
	}
	// hadFresh && len(matched)==0 → unrelated cached ids; live CanAccept.
	// !hadFresh → empty/expired cache; live CanAccept.
	return h.CanAccept(ids, h.TaskEngine)
}

func (h *taskTypeHandler) shouldLogAcceptRefusal(now time.Time) bool {
	if !h.lastAcceptRefusalLog.IsZero() && now.Sub(h.lastAcceptRefusalLog) < time.Minute {
		return false
	}
	h.lastAcceptRefusalLog = now
	return true
}

// toInt64s / toTaskIDs bridge the TaskID (int) and int64 domains used by
// acceptcache. The internal package is typed on int64 to avoid an import
// cycle with harmonytask.
func toInt64s(in []TaskID) []int64 {
	out := make([]int64, len(in))
	for i, id := range in {
		out[i] = int64(id)
	}
	return out
}

func toTaskIDs(in []int64) []TaskID {
	out := make([]TaskID, len(in))
	for i, id := range in {
		out[i] = TaskID(id)
	}
	return out
}
