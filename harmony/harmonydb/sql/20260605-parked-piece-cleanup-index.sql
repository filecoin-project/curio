-- Replace the partial cleanup index with a composite index over the columns
-- used by the cleanup candidate query. The old partial index keyed only id,
-- while ref_count and cleanup_task_id controlled index membership.

DROP INDEX IF EXISTS idx_parked_pieces_cleanup_eligible;
DROP INDEX IF EXISTS idx_parked_pieces_cleanup_null;
DROP INDEX IF EXISTS idx_parked_pieces_cleanup_pending;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_eligible
    ON parked_pieces (ref_count, cleanup_task_id, id);
