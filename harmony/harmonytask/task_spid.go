package harmonytask

// TaskSPID associates one task with a provider; a task may have several.
type TaskSPID struct {
	TaskID int64 `db:"task_id"`
	SPID   int64 `db:"sp_id"`
}
