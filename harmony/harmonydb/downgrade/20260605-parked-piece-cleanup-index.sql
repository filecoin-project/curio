DROP INDEX IF EXISTS idx_parked_pieces_cleanup_eligible;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_eligible
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL AND ref_count = 0;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_null
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_pending
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL;
