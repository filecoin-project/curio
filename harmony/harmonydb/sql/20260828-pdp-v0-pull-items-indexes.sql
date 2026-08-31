CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_incomplete
    ON pdp_piece_pull_items (created_at ASC)
    WHERE complete = FALSE;

CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_task_id
    ON pdp_piece_pull_items (task_id);
