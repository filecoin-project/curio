-- Revert 20260424-seal-poller-indexes.sql

DROP INDEX IF EXISTS idx_parked_pieces_pending;
DROP INDEX IF EXISTS idx_sdr_pipeline_commit_ready;
DROP INDEX IF EXISTS idx_sdr_pipeline_precommit_ready;
DROP INDEX IF EXISTS idx_sdr_pipeline_active;
