-- Rename terminated_at_epoch to unrecoverable_proving_failure_epoch
-- This column tracks when a dataset had an unrecoverable proving failure, not necessarily termination

ALTER TABLE pdp_data_sets RENAME COLUMN terminated_at_epoch TO unrecoverable_proving_failure_epoch;

COMMENT ON COLUMN pdp_data_sets.unrecoverable_proving_failure_epoch IS 'Block height at which an unrecoverable proving failure was detected; NULL if active';
