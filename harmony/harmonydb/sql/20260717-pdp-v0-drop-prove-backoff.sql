-- PDPv0 no longer uses dataset-local prove retry counters/backoff.
-- Contract reverts are handled by explicit categories and Harmony task retry.
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS consecutive_prove_failures;
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS next_prove_attempt_at;
