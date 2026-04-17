package harmonytask

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runnowflags"
)

// singletonRunNow is the package-scoped registry of per-task run-now flags.
// The singletonRunNowPoller sets a flag when it sees a run_now_request row
// in the DB; SingletonTaskAdder reads and clears it to bypass the normal
// minInterval on the next tick.
//
// The underlying map and mutex live inside runnowflags.Registry, so nothing
// in this package can touch them directly. All access is through method
// calls that lock correctly.
var singletonRunNow = runnowflags.New()

func SingletonTaskAdder(minInterval time.Duration, task TaskInterface) func(AddTaskFunc) error {
	// taskName and runNowFlag are resolved lazily to avoid infinite recursion:
	// SingletonTaskAdder is called from TypeDetails(), which would call
	// task.TypeDetails().Name, which calls SingletonTaskAdder again.
	var taskName string
	var runNowFlag *atomic.Bool
	var initOnce sync.Once

	var lastCall time.Time
	var lk sync.Mutex

	return func(add AddTaskFunc) error {
		initOnce.Do(func() {
			taskName = task.TypeDetails().Name
			runNowFlag = singletonRunNow.Flag(taskName)
		})

		lk.Lock()
		defer lk.Unlock()

		// Check if the global poller signaled a run-now request.
		// Swap to false so we only bypass the interval once.
		runNowTriggered := runNowFlag.Swap(false)

		if !runNowTriggered && time.Since(lastCall) < minInterval {
			return nil
		}
		lastCall = time.Now()

		add(func(taskID TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			var existingTaskID *int64
			var lastRunTime time.Time
			var runNowRequest bool
			var shouldRun bool

			now := time.Now()

			err = tx.QueryRow(`SELECT task_id, last_run_time, run_now_request FROM harmony_task_singletons WHERE task_name = $1`, taskName).Scan(&existingTaskID, &lastRunTime, &runNowRequest)
			if errors.Is(err, pgx.ErrNoRows) {
				shouldRun = true
			} else if err != nil {
				return false, err
			} else {
				taskIsRunning := false

				if existingTaskID != nil {
					var htTaskID *int64
					err = tx.QueryRow(`SELECT id FROM harmony_task WHERE id = $1 AND name = $2`, existingTaskID, taskName).Scan(&htTaskID)
					if errors.Is(err, pgx.ErrNoRows) {
						taskIsRunning = false
					} else if err != nil {
						return false, err
					} else {
						taskIsRunning = htTaskID != nil
					}
				}

				if !taskIsRunning {
					shouldRun = runNowRequest || lastRunTime.Add(minInterval).Before(now)
				}
				// If task IS running, leave run_now_request set so it fires
				// after the current run completes on the next cycle.
			}

			if !shouldRun {
				return false, nil
			}

			// Conditionally insert or update the task entry, clearing run_now_request
			n, err := tx.Exec(`
                INSERT INTO harmony_task_singletons (task_name, task_id, last_run_time, run_now_request)
				VALUES ($1, $2, $3, FALSE)
				ON CONFLICT (task_name) DO UPDATE
				SET task_id = 
					CASE 
						-- If task_id is NULL, or if it exists but does not match an active task_name, update it
						WHEN harmony_task_singletons.task_id IS NULL 
						OR NOT EXISTS (
							SELECT 1 FROM harmony_task 
							WHERE id = harmony_task_singletons.task_id 
							  AND name = harmony_task_singletons.task_name
						) 
						THEN $2
						ELSE harmony_task_singletons.task_id -- Otherwise, keep the existing task_id
					END,
					last_run_time = 
					CASE 
						WHEN harmony_task_singletons.task_id IS DISTINCT FROM $2 THEN $3 -- Only update when task_id changes
						ELSE harmony_task_singletons.last_run_time -- Keep the existing value
					END,
					run_now_request = FALSE`, taskName, taskID, now)
			if err != nil {
				return false, err
			}
			return n > 0, nil
		})
		return nil
	}
}
