CREATE INDEX IF NOT EXISTS harmony_task_history_recent_task_result_idx
    ON harmony_task_history (work_end DESC, name ASC, task_id ASC, result ASC);