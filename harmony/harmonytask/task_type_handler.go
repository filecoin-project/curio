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
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

var log = logging.Logger("harmonytask")

type PipelineTask interface {
	GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error)
}

type taskTypeHandler struct {
	TaskInterface
	TaskTypeDetails
	TaskEngine      *TaskEngine
	storageFailures map[TaskID]time.Time

	// for caching CanAccept() results to db unnecessarily.
	acceptedTasks    []TaskID
	lastAcceptedTime time.Time
}

const CAN_ACCEPT_CACHE_TTL = 60 * time.Second

// Anti-hammering of storage claims.
const STORAGE_FAILURE_TIMEOUT = 3 * time.Minute

const (
	WorkSourcePoller   = "poller"
	WorkSourceRecover  = "recovered"
	WorkSourceIAmBored = "bored"
	WorkSourceAdded    = "added"
)

// considerWork is called to attempt to start work on a task-id of this task type.
// It presumes single-threaded calling, so there should not be a multi-threaded re-entry.
// The only caller should be the one work poller thread. This does spin off other threads,
// but those should not considerWork. Work completing may lower the resource numbers
// unexpectedly, but that will not invalidate work being already able to fit.
//
// Logic for multiple task IDs:
// Runs CanAccept as few times as possible and respects list order.
// The headroom (maxes, resources) becomes a SQL LIMIT so we can get work anywhere in the list.
// Avoids caching CanAccept() results as priority may change
// Disk resources are claimed AFTER taking the tasks because they may have different storage paths so they can't update.
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
	// Were any accepted already? Clear stale cache.
	if time.Since(h.lastAcceptedTime) > CAN_ACCEPT_CACHE_TTL {
		h.acceptedTasks = nil
	}

	// 3. What does the impl say?
	var tIDs []TaskID
	if len(h.acceptedTasks) > 0 {
		// Use cached CanAccept results — filter for tasks still in this batch
		tIDs = lo.Filter(h.acceptedTasks, func(tID TaskID, _ int) bool {
			return lo.Contains(ids, tID)
		})
		h.acceptedTasks = nil // consume the cache; scheduler loop re-polls for fresh work
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

	// if recovering we don't need to try to claim anything because those tasks are already claimed by us
	if from != WorkSourceRecover {
		// 4. Can we claim the work for our hostname?
		var tasksAccepted []TaskID
		// Limit at the last possible moment in SQL AFTER we know it's unclaimed: this opens us to get work anywhere in the list.
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
		if len(tasksAccepted) != len(tIDs) {
			h.acceptedTasks = lo.Filter(tIDs, func(tID TaskID, _ int) bool {
				return !lo.Contains(tasksAccepted, tID)
			})
			h.lastAcceptedTime = time.Now()
			tIDs = tasksAccepted // update tIDs to the accepted tasks
		}
	}

	releaseStorage := make([]func(), len(tIDs))

	if h.Cost.Storage != nil {
		// Risk of this late-store: A "full macihne" will claim tasks, then back out of them.
		failedTIDs := []TaskID{}
		goodTIDs := []TaskID{}
		releaseStorage = []func(){}
		for _, tID := range tIDs {
			markComplete, err := h.Cost.Claim(int(tID)) // Accepted tasks IDs are known now.
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
			tIDs = goodTIDs // releaseStorage will match this now.
			log.Errorw("did not accept task", "task_ids", failedTIDs, "reason", "storage claim failed", "name", h.Name)
			// This is not a task failure, so just reset the owner_id.
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

			done, doErr = h.Do(tID, func() bool {
				if taskhelp.IsBackgroundTask(h.Name) || h.CanYield {
					if h.TaskEngine.yieldBackground.Load() {
						log.Infow("yielding background task", "name", h.Name, "id", tID)
						return false
					}
				}

				var owner int
				// Background here because we don't want GracefulRestart to block this save.
				err := h.TaskEngine.db.QueryRow(context.Background(),
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

func (h *taskTypeHandler) recordCompletion(tID TaskID, sectorID *abi.SectorID, workStart time.Time, done bool, doErr error) {
	workEnd := time.Now()
	retryWait := time.Millisecond * 100

	{
		// metrics

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

// MaxHeadroom controls the maximum number of tasks per type that can be active on this node.
// It is exported and mutable so it can be configured via the HARMONY_MAX_TASKS_PER_TYPE
// environment variable during process initialization.
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
	ramHeadroom := r.Ram / h.Cost.Ram
	if ramHeadroom < uint64(headroom) {
		headroom = int(ramHeadroom)
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
