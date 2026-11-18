-- A data set enters the deletion pipeline only when a client or the storage
-- provider operator asks for it. client_requested_termination records the
-- former, operator_requested_deletion the latter.
ALTER TABLE pdp_delete_data_set
    ADD COLUMN IF NOT EXISTS operator_requested_deletion BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE pdp_data_sets
    ADD COLUMN IF NOT EXISTS rail_end_epoch BIGINT;

COMMENT ON COLUMN pdp_data_sets.rail_end_epoch IS
    'End epoch of the payment rail, recorded when Curio first sees the rail terminated on chain';

-- Rows that nobody requested are not pipeline entries. Those that have not
-- reached the deleteDataSet stage are removed; rows already in that stage
-- finish their cleanup.
DELETE FROM pdp_delete_data_set
WHERE client_requested_termination = FALSE
  AND operator_requested_deletion = FALSE
  AND after_delete_data_set = FALSE
  AND delete_data_set_task_id IS NULL;
