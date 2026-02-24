ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS terminated_at_epoch;
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS consecutive_prove_failures;
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS next_prove_attempt_at;
