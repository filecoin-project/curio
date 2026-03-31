package harmonytask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

var log = logging.Logger("harmonytask")

type PipelineTask interface {
	GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error)
}

// taskTypeHandler wraps a TaskInterface with scheduling metadata and runtime
// state. Each registered task type gets one handler instance. The handler
// manages concurrency limits, resource accounting, CanAccept caching, and
// storage failure tracking for its task type.
type taskTypeHandler struct {
	TaskInterface
	TaskTypeDetails
	TaskEngine      *TaskEngine
	storageFailures map[TaskID]time.Time // taskID -> last failure time; prevents hammering known-bad storage

	// CanAccept result cache: the background poller pre-computes CanAccept
	// off the scheduler thread and writes results here via SetAcceptCache().
	// The scheduler consumes them on the next considerWork call, avoiding a
	// redundant (and potentially expensive) CanAccept call on the hot path.
	// Entries expire after CAN_ACCEPT_CACHE_TTL to prevent stale decisions.
	//
	// acceptMu protects acceptedTasks and lastAcceptedTime because the
	// background poller goroutine writes them while the scheduler goroutine
	// reads/clears them. This is the only cross-thread state in the handler.
	acceptMu         sync.Mutex
	acceptedTasks    []TaskID
	lastAcceptedTime time.Time
}

// CAN_ACCEPT_CACHE_TTL controls how long pre-computed CanAccept results remain
// valid. After this duration, the cache is discarded and CanAccept is called
// fresh. This balances DB load reduction against decision freshness.
// This is to avoid trampling retries, but a less chatty mechanism is possible.
const CAN_ACCEPT_CACHE_TTL = 60 * time.Second

// SetAcceptCache is called by the background poller goroutine to write
// pre-computed CanAccept results directly into the handler's cache.
// The scheduler thread reads and clears this cache in considerWork().
func (h *taskTypeHandler) SetAcceptCache(accepted []TaskID) {
	h.acceptMu.Lock()
	h.acceptedTasks = append(h.acceptedTasks, accepted...)
	h.lastAcceptedTime = time.Now()
	h.acceptMu.Unlock()
}

// STORAGE_FAILURE_TIMEOUT prevents repeatedly trying to claim storage for a
// task that just failed storage allocation. The task is retried after this
// cooldown or on the next process restart.
const STORAGE_FAILURE_TIMEOUT = 3 * time.Minute

// WorkSource constants identify how work was discovered, passed to
// TaskInterface.CanAccept via TaskEngine.WorkOrigin so implementations
// can make source-aware decisions (e.g., prefer locally-added work).
const (
	WorkSourcePoller   = "poller"
	WorkSourceRecover  = "recovered"
	WorkSourceIAmBored = "bored"
	WorkSourceAdded    = "added"
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
		return true // stop looking for takers
	}

	// 1. Can we do any more of this task type?
	// NOTE: 0 is the default value, so this way people don't need to worry about
	// this setting unless they want to limit the number of tasks of this type.
	if h.Max.AtMax() {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at max already")
		return false
	}

	// 2. Can we do any more work? From here onward, we presume the resource
	// story will not change, so single-threaded calling is best.
	maxAcceptable, err := h.AssertMachineHasCapacity()
	if err != nil {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at capacity already: "+err.Error())
		return false
	}

	h.TaskEngine.WorkOrigin = from

	ids := lo.Map(tasks, func(t task, _ int) TaskID {
		return t.ID
	})
	// 3. CanAccept: either use cached results (pre-computed by the background
	// poller off the scheduler thread) or call fresh. The cache avoids
	// redundant CanAccept calls when the poller already did this work,
	// which is especially valuable for CanAccept impls that check disk
	// paths or query external services.
	var tIDs []TaskID
	var cached []TaskID
	h.acceptMu.Lock()
	if time.Since(h.lastAcceptedTime) > CAN_ACCEPT_CACHE_TTL {
		h.acceptedTasks = nil
	}
	if len(h.acceptedTasks) > 0 {
		cached = h.acceptedTasks
		h.acceptedTasks = nil // consume the cache; next poll will refresh
	}
	h.acceptMu.Unlock()

	if len(cached) > 0 {
		tIDs = lo.Filter(cached, func(tID TaskID, _ int) bool {
			return lo.Contains(ids, tID)
		})
	} else {
		tIDs, err = h.CanAccept(ids, h.TaskEngine)
	}

	h.TaskEngine.WorkOrigin = ""

	if err != nil {
		log.Error(err)
		return false
	}
	if len(tIDs) == 0 {
		log.Infow("did not accept task", "task_ids", ids, "reason", "CanAccept() refused", "name", h.Name)
		return false
	}

	headroomUntilMax := h.Max.Headroom()
	if maxAcceptable > headroomUntilMax {
		maxAcceptable = headroomUntilMax
	}

	// filter storage failures here
	tIDs = lo.Filter(tIDs, func(tID TaskID, _ int) bool {
		v, ok := h.storageFailures[tID]
		if !ok {
			return true
		}
		if time.Since(v) > STORAGE_FAILURE_TIMEOUT { // Retry in an hour or next reboot.
			delete(h.storageFailures, tID)
			return true
		}
		return false // Lets not hammer Tasks we know are failing.
	})

	// Recovered tasks are already claimed (owner_id = us) — skip the SQL claim.
	if from != WorkSourceRecover {
		// 4. SQL claim: atomically set owner_id for unclaimed tasks using
		// SKIP LOCKED to avoid blocking on tasks being claimed by other nodes.
		// The LIMIT is applied at the SQL level (not in Go) so we can claim
		// tasks from anywhere in the CanAccept-approved list, not just the first N.
		var tasksAccepted []TaskID
		err := h.TaskEngine.db.Select(h.TaskEngine.ctx, &tasksAccepted, `
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
		RETURNING t.id;`, h.TaskEngine.ownerID, tIDs, maxAcceptable)

		if err != nil {
			log.Error(err)
			return false
		}
		if len(tasksAccepted) == 0 {
			log.Infow("did not accept task", "task_id", tIDs, "reason", "already Taken", "name", h.Name)

			return false
		}
		// If we claimed fewer than CanAccept approved, cache the remainder.
		// They'll be tried on the next scheduling pass without re-calling
		// CanAccept, reducing unnecessary work.
		if len(tasksAccepted) != len(tIDs) {
			remainder := lo.Filter(tIDs, func(tID TaskID, _ int) bool {
				return !lo.Contains(tasksAccepted, tID)
			})
			h.acceptMu.Lock()
			h.acceptedTasks = remainder
			h.lastAcceptedTime = time.Now()
			h.acceptMu.Unlock()
			tIDs = tasksAccepted
		}
	}

	// 5. Storage claim: for tasks with disk requirements, claim storage paths
	// now that we know the specific task IDs. This is intentionally late-bound
	// because different tasks may map to different storage paths. The trade-off:
	// a fully loaded machine may claim tasks in SQL then fail storage and have
	// to release them — but this is rare and self-healing.
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
			// Release the SQL claims for tasks that failed storage allocation.
			_, err := h.TaskEngine.db.Exec(h.TaskEngine.ctx, `UPDATE harmony_task SET owner_id = NULL WHERE id = ANY($1)`, failedTIDs)
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

	// 6. Launch a goroutine for each claimed task. Each goroutine:
	//   - Emits TaskStarted so the scheduler removes it from available tasks
	//   - Calls Do() with a stillOwned callback for single-writer safety
	//   - On completion, records results to DB and emits TaskCompleted
	//   - On failure, emits TaskNew to re-add the task for retry
	i := 0
	for _, tID := range tIDs {
		go func(tID TaskID, releaseStorage func()) {
			eventEmitter.EmitTaskStarted(h.Name, tID)
			var done bool
			var doErr error
			workStart := time.Now()

			var sectorID *abi.SectorID
			if ht, ok := h.TaskInterface.(PipelineTask); ok {
				sectorID, err = ht.GetSectorID(h.TaskEngine.db, int64(tID))
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
				h.Max.Add(-1)

				if releaseStorage != nil {
					releaseStorage()
				}
				h.recordCompletion(tID, sectorID, workStart, done, doErr)
				eventEmitter.EmitTaskCompleted(h.Name)
				if !done { // it was a failure, so we need to retry
					for _, t := range tasks {
						if t.ID == tID {
							eventEmitter.EmitTaskNew(h.Name, t)
						}
					}
				}
			}()

			taskCtx, taskCancel := context.WithCancel(h.TaskEngine.ctx)
			defer taskCancel()

			done, doErr = h.Do(taskCtx, tID, func() bool {
				if taskhelp.IsBackgroundTask(h.Name) || h.CanYield {
					if h.TaskEngine.yieldBackground.Load() {
						log.Infow("yielding background task", "name", h.Name, "id", tID)
						return false
					}
				}

				var owner int
				// Keep ownership checks tied to the same task context used by Do().
				err := h.TaskEngine.db.QueryRow(taskCtx,
					`SELECT owner_id FROM harmony_task WHERE id=$1`, tID).Scan(&owner)
				if err != nil {
					log.Error("Cannot determine ownership: ", err)
					return false
				}
				return owner == h.TaskEngine.ownerID
			})
			if doErr != nil {
				log.Errorw("Do() returned error", "type", h.Name, "id", strconv.Itoa(int(tID)), "error", doErr)
			}
		}(tID, releaseStorage[i])
		i++
	}
	return true
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
func (h *taskTypeHandler) recordCompletion(tID TaskID, sectorID *abi.SectorID, workStart time.Time, done bool, doErr error) {
	workEnd := time.Now()
	retryWait := time.Millisecond * 100

	{

		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(taskNameTag, h.Name),
		}, TaskMeasures.ActiveTasks.M(int64(h.Max.ActiveThis())))

		duration := workEnd.Sub(workStart).Seconds()
		TaskMeasures.TaskDuration.Observe(duration)

		if done {
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
				tag.Upsert(taskNameTag, h.Name),
			}, TaskMeasures.TasksCompleted.M(1))
		} else {
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
				tag.Upsert(taskNameTag, h.Name),
			}, TaskMeasures.TasksFailed.M(1))
		}
	}

retryRecordCompletion:
	// Use Background context: recordCompletion MUST finish even during graceful shutdown.
	cm, err := h.TaskEngine.db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		var postedTime time.Time
		var retries uint
		err := tx.QueryRow(`SELECT posted_time, retries FROM harmony_task WHERE id=$1`, tID).Scan(&postedTime, &retries)
		if err != nil {
			return false, fmt.Errorf("could not log completion: %w ", err)
		}
		result := "unspecified error"
		if done {
			_, err = tx.Exec("DELETE FROM harmony_task WHERE id=$1", tID)
			if err != nil {

				return false, fmt.Errorf("could not log completion: %w", err)
			}
			result = ""
			if doErr != nil {
				result = "non-failing error: " + doErr.Error()
			}
		} else {
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
				// Note: Extra Info is left laying around for later review & clean-up
			} else {
				_, err := tx.Exec(`UPDATE harmony_task SET owner_id=NULL, retries=$1, update_time=CURRENT_TIMESTAMP  WHERE id=$2`, retries+1, tID)
				if err != nil {
					return false, fmt.Errorf("could not disown failed task: %v %v", tID, err)
				}
			}
		}

		var hid int
		err = tx.QueryRow(`INSERT INTO harmony_task_history 
									 (task_id, name, posted, work_start, work_end, result, completed_by_host_and_port, err)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, tID, h.Name, postedTime.UTC(), workStart.UTC(), workEnd.UTC(), done, h.TaskEngine.hostAndPort, result).Scan(&hid)
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
	// This MUST complete or keep getting retried until it does. If restarted, it will be cleaned-up, so no need for alt persistence.
	if err != nil || !cm {
		time.Sleep(retryWait)
		retryWait *= 2
		if retryWait > time.Second*10 {
			log.Error("Could not record completion (retrying): ", err)
		}
		goto retryRecordCompletion
	}
}

// MaxHeadroom limits how many tasks of a single type can be accepted in one
// considerWork call. This prevents a single task type from monopolizing all
// resources. Configurable via HARMONY_MAX_TASKS_PER_TYPE env var.
var MaxHeadroom = 100

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
		MaxHeadroom = v
	}
}

// AssertMachineHasCapacity checks whether this node has sufficient resources
// (CPU, RAM, GPU, storage) to run another task of this type. Returns the
// maximum number of additional tasks that could fit (headroom), constrained
// by the scarcest resource. Returns 0 with an error if no capacity exists.
func (h *taskTypeHandler) AssertMachineHasCapacity() (int, error) {
	r := h.TaskEngine.ResourcesAvailable()
	headroom := MaxHeadroom
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

	if h.Cost.Storage != nil { // Counts > 1 handled by CanAccept()
		if !h.Cost.HasCapacity() {
			return 0, errors.New("Did not accept " + h.Name + " task: out of available Storage")
		}
	}
	return headroom, nil
}
