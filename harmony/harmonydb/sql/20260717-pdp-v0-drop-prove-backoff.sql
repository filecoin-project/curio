-- PDPv0 no longer uses dataset-local prove retry counters/backoff.
-- Contract reverts are handled by explicit categories and Harmony task retry.
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS consecutive_prove_failures;
ALTER TABLE pdp_data_sets DROP COLUMN IF EXISTS next_prove_attempt_at;

ALTER TABLE pdp_data_sets
    ADD COLUMN IF NOT EXISTS pp_reconcile_needed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_pdp_data_sets_pp_reconcile_needed
    ON pdp_data_sets (id)
    WHERE pp_reconcile_needed = TRUE;
