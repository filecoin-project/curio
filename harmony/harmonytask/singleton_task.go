package harmonytask

import (
	"errors"
	"time"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/passcall"
)

func SingletonTaskAdder(minInterval time.Duration, task TaskInterface) func(AddTaskFunc) error {
	return passcall.Every(minInterval, func(add AddTaskFunc) error {
		taskName := task.TypeDetails().Name

		add(func(taskID TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			var existingTaskID *int64
			var lastRunTime time.Time
			var shouldRun bool

			now := time.Now()

			// Query to check the existing task entry
			err = tx.QueryRow(`SELECT task_id, last_run_time FROM harmony_task_singletons WHERE task_name = $1`, taskName).Scan(&existingTaskID, &lastRunTime)
			if errors.Is(err, pgx.ErrNoRows) {
				// No existing record → Task should run
				shouldRun = true
			} else if err != nil {
				return false, err // Return actual error
			} else if existingTaskID == nil {
				// No existing record → Task should run
				shouldRun = lastRunTime.Add(minInterval).Before(now)
			} else {
				// make sure the task is still active
				var htTaskID *int64
				err = tx.QueryRow(`SELECT id FROM harmony_task WHERE id = $1 AND name = $2`, existingTaskID, taskName).Scan(&htTaskID)
				if errors.Is(err, pgx.ErrNoRows) {
					// Task no longer exists, should run
					shouldRun = lastRunTime.Add(minInterval).Before(now)
				} else if err != nil {
					return false, err
				} else {
					shouldRun = htTaskID == nil && lastRunTime.Add(minInterval).Before(now)
				}
			}

			if !shouldRun {
				return false, nil
			}

			// Conditionally insert or update the task entry
			n, err := tx.Exec(`
                INSERT INTO harmony_task_singletons (task_name, task_id, last_run_time)
				VALUES ($1, $2, $3)
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
					END`, taskName, taskID, now)
			if err != nil {
				return false, err
			}
			return n > 0, nil
		})
		return nil
	})
}
