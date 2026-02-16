-- Add save cache task columns to pdp_piecerefs (mirrors indexing pattern)
ALTER TABLE pdp_piecerefs ADD COLUMN needs_save_cache BOOLEAN DEFAULT FALSE;
ALTER TABLE pdp_piecerefs ADD COLUMN save_cache_task_id BIGINT DEFAULT NULL;

-- Index for save cache task scheduling
CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_save_cache_pending
    ON pdp_piecerefs (created_at ASC)
    WHERE save_cache_task_id IS NULL AND needs_save_cache = TRUE;
