DROP INDEX IF EXISTS idx_ipni_pending_sync;
ALTER TABLE ipni DROP COLUMN IF EXISTS indexed_at;
