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
	storageFailures map[TaskID]time.Time

	// --- concurrent state, encapsulated behind typed APIs ---
	//
	// The mutex and backing store for each of these lives inside an internal
	// sub-package, so nothing in this package can reach them without going
	// through methods that lock correctly.
	running *runregistry.Registry
	accept  *acceptcache.Cache
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
	workSourcePoller  = "poller"
	workSourceRecover = "recovered"
	workSourcePreempt = "preempt"
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
	if len(tasks) == 0 {
		return true
	}

	// Skip IDs already running on this node. After a successful claim,
	// running.Start happens before considerWork returns, so a later waterfall
	// (TaskCompleted / bundle / tryStart) must not re-claim and log "already Taken".
	if from != workSourceRecover {
		tasks = lo.Filter(tasks, func(t task, _ int) bool {
			_, running := h.running.Get(int64(t.ID))
			return !running
		})
		if len(tasks) == 0 {
			return true
		}
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

	ids := lo.Map(tasks, func(t task, _ int) TaskID {
		return t.ID
	})
	tIDs, err := h.resolveAcceptedIDs(ids)
	if err != nil {
		log.Error(err)
		return false
	}
	if len(tIDs) == 0 {
		log.Infow("did not accept task", "task_ids", ids, "reason", "CanAccept() refused", "name", h.Name)
		return false
	}

	tIDs = reorderTaskIDsByPostedOrder(tasks, tIDs)

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

	if from != workSourceRecover {
		var tasksAccepted []TaskID
		err := h.TaskEngine.cfg.db.Select(h.TaskEngine.cfg.ctx, &tasksAccepted, `
		WITH candidates AS (
			SELECT t.id
			FROM harmony_task t
			JOIN unnest($2::bigint[]) AS x(id) ON x.id = t.id
			WHERE t.owner_id IS NULL
			ORDER BY array_position($2, t.id::bigint)
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE harmony_task t
		SET owner_id = $1
		FROM candidates c
		WHERE t.id = c.id
		RETURNING t.id;`, h.TaskEngine.cfg.ownerID, tIDs, maxAcceptable)

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
		if len(tasksAccepted) != len(tIDs) {
			remainder := lo.Filter(tIDs, func(tID TaskID, _ int) bool {
				return !lo.Contains(tasksAccepted, tID)
			})
			h.accept.Add(toInt64s(remainder))
			tIDs = tasksAccepted
		}
	}

	releaseStorage := make([]func(), len(tIDs))

	if h.Cost.Storage != nil {
		failedTIDs := []TaskID{}
		goodTIDs := []TaskID{}
		releaseStorage = []func(){}
		for _, tID := range tIDs {
			markComplete, err := h.Cost.Claim(int(tID))
			if err != nil {
				failedTIDs = append(failedTIDs, tID)
				h.storageFailures[tID] = time.Now()
				continue
			}
			goodTIDs = append(goodTIDs, tID)
			releaseStorage = append(releaseStorage, func() {
				if err := markComplete(); err != nil {
					log.Errorw("Could not release storage", "error", err)
				}
			})
		}
		if len(failedTIDs) > 0 {
			tIDs = goodTIDs
			log.Errorw("did not accept task", "task_ids", failedTIDs, "reason", "storage claim failed", "name", h.Name)
			_, err := h.TaskEngine.cfg.db.Exec(h.TaskEngine.cfg.ctx, `UPDATE harmony_task SET owner_id = NULL WHERE id = ANY($1)`, failedTIDs)
			if err != nil {
				log.Errorw("Could not reset failed tasks", "error", err)
			}
			if len(goodTIDs) == 0 {
				return false
			}
		}
	} else {
		for i := range tIDs {
			releaseStorage[i] = func() {}
		}
	}

	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, h.Name),
		tag.Upsert(sourceTag, from),
	}, TaskMeasures.TasksStarted.M(int64(len(tIDs))))

	h.Max.Add(len(tIDs))
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, h.Name),
	}, TaskMeasures.ActiveTasks.M(int64(h.Max.ActiveThis())))

	// Drop claimed IDs from the scheduler's available set immediately (same
	// thread). Waiting for async TaskStarted left a window where another
	// waterfall re-tried the SQL claim and logged "already Taken".
	eventEmitter.NoteClaimed(h.Name, tIDs)

	// 6. Launch a goroutine for each claimed task. Each goroutine:
	//   - Emits TaskStarted so peers are notified (local map already cleared)
	//   - Calls Do() with a stillOwned callback for single-writer safety
	//   - On completion, records results to DB and emits TaskCompleted
	//   - On failure, emits TaskNew to re-add the task for retry
	i := 0
	for _, tID := range tIDs {
		// Derive from context.Background(), not the engine ctx: a graceful
		// shutdown (which cancels the engine ctx to stop picking up new work)
		// must NOT cancel tasks already running, or in-flight time-sensitive
		// work (WinningPost/WindowPost) would be aborted mid-flight even though
		// GracefullyTerminate explicitly waits for it to finish. The only things
		// that cancel a running task are preemption (handle.Preempt) and the
		// task's own completion (the deferred taskCancel below).
		taskCtx, taskCancel := context.WithCancel(context.Background())
		meta := &completionMeta{vals: make(map[any]any)}
		taskCtx = context.WithValue(taskCtx, completionMetaKey{}, meta)
		handle := h.running.Start(int64(tID), taskCancel)

		go func(tID TaskID, releaseStorage func(), handle *runregistry.Handle) {
			eventEmitter.EmitTaskStarted(h.Name, tID)
			var done bool
			var doErr error
			workStart := handle.StartTime()

			var sectorID *abi.SectorID
			if ht, ok := h.TaskInterface.(pipelineTask); ok {
				sectorID, err = ht.GetSectorID(h.TaskEngine.cfg.db, int64(tID))
				if err != nil {
					log.Errorw("Could not get sector ID", "task", h.Name, "id", tID, "error", err)
				}
			}

			log.Infow("Beginning work on Task", "id", tID, "from", from, "name", h.Name, "sector", sectorID)

			defer func() {
				if r := recover(); r != nil {
					stackSlice := make([]byte, 4092)
					sz := runtime.Stack(stackSlice, false)
					log.Error("Recovered from a serious error "+
						"while processing "+h.Name+" task "+strconv.Itoa(int(tID))+": ", r,
						" Stack: ", string(stackSlice[:sz]))
				}
				taskCancel()

				preempted := handle.IsPreempted()
				h.running.Finish(int64(tID))

				h.Max.Add(-1)

				if releaseStorage != nil {
					releaseStorage()
				}
				h.recordCompletion(tID, sectorID, workStart, done, doErr, preempted)
				success := done && doErr == nil
				if success {
					h.TaskEngine.invokeTaskCompleteCallbacks(h.Name, meta, tID)
				}
				eventEmitter.EmitTaskCompleted(h.Name, success)
				if !done {
					for _, t := range tasks {
						if t.ID == tID {
							h.emitRetryTask(eventEmitter, t, preempted)
							break
						}
					}
				}
			}()

			defer taskCancel()

			done, doErr = h.Do(taskCtx, tID, func() bool {
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
			if doErr != nil {
				log.Errorw("Do() returned error", "type", h.Name, "id", strconv.Itoa(int(tID)), "error", doErr)
			}
		}(tID, releaseStorage[i], handle)
		i++
	}
	return true
}

// emitRetryTask re-adds a failed or preempted task to the scheduler after
// recordCompletion. Failed tasks sleep RetryWait before emitting so the task is
// not visible to scheduling (or peers) until the backoff elapses. Tasks dropped
// after MaxFailures are not re-emitted.
func (h *taskTypeHandler) emitRetryTask(eventEmitter eventEmitter, base task, preempted bool) {
	if base.ID == 0 {
		return
	}

	if preempted {
		eventEmitter.EmitTaskNew(h.Name, base)
		return
	}

	if h.MaxFailures > 0 && base.Retries >= int(h.MaxFailures)-1 {
		return
	}

	retry := base
	retry.Retries++
	retry.UpdateTime = time.Now().UTC()

	var wait time.Duration
	if h.RetryWait != nil && retry.Retries > 0 {
		wait = h.RetryWait(retry.Retries)
	}
	go func() {
		time.Sleep(wait)
		eventEmitter.EmitTaskNew(h.Name, retry)
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
func (h *taskTypeHandler) recordCompletion(tID TaskID, sectorID *abi.SectorID, workStart time.Time, done bool, doErr error, preempted bool) {
	workEnd := time.Now()
	retryWait := time.Millisecond * 100

	{
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(taskNameTag, h.Name),
		}, TaskMeasures.ActiveTasks.M(int64(h.Max.ActiveThis())))

		duration := workEnd.Sub(workStart).Seconds()
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(taskNameTag, h.Name),
		}, TaskMeasures.TaskDuration.M(duration))

		if done {
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
				tag.Upsert(taskNameTag, h.Name),
			}, TaskMeasures.TasksCompleted.M(1))
		} else if !preempted {
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
				tag.Upsert(taskNameTag, h.Name),
			}, TaskMeasures.TasksFailed.M(1))
		}
	}

	var waitStartTime time.Time
retryRecordCompletion:
	cm, err := h.TaskEngine.cfg.db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		var postedTime time.Time
		var retries uint
		var updateTime time.Time
		err := tx.QueryRow(`SELECT posted_time, update_time, retries FROM harmony_task WHERE id=$1`, tID).Scan(&postedTime, &updateTime, &retries)
		if err != nil {
			return false, fmt.Errorf("could not log completion: %w ", err)
		}
		waitStartTime = postedTime
		if retries > 0 {
			waitStartTime = updateTime
		}
		result := ""
		switch {
		case done:
			_, err = tx.Exec("DELETE FROM harmony_task WHERE id=$1", tID)
			if err != nil {
				return false, fmt.Errorf("could not log completion: %w", err)
			}
			result = ""
			if doErr != nil {
				result = "non-failing error: " + doErr.Error()
			}
		case preempted:
			_, err = tx.Exec(`UPDATE harmony_task SET owner_id=NULL, update_time=CURRENT_TIMESTAMP WHERE id=$1`, tID)
			if err != nil {
				return false, fmt.Errorf("could not release preempted task: %v %v", tID, err)
			}
			result = "preempted"
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
				_, err = tx.Exec("DELETE FROM harmony_task WHERE id=$1", tID)
				if err != nil {
					return false, fmt.Errorf("could not delete failed job: %w", err)
				}
			} else {
				_, err = tx.Exec(`UPDATE harmony_task SET owner_id=NULL, retries=$1, update_time=CURRENT_TIMESTAMP WHERE id=$2`, retries+1, tID)
				if err != nil {
					return false, fmt.Errorf("could not disown failed task: %v %v", tID, err)
				}
			}
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

		return true, nil
	})
	if err != nil || !cm {
		time.Sleep(retryWait)
		retryWait *= 2
		if retryWait > time.Second*10 {
			log.Error("Could not record completion (retrying): ", err)
		}
		goto retryRecordCompletion
	}

	scheduledWait := workStart.Sub(waitStartTime).Seconds()
	if scheduledWait < 0 {
		scheduledWait = 0
	}
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(taskNameTag, h.Name),
	}, TaskMeasures.TaskScheduledWait.M(scheduledWait))
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
