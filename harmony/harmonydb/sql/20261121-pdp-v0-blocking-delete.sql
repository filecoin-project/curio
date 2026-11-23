-- A data set enters the deletion pipeline only when a client or the storage
-- provider operator asks for it.
ALTER TABLE pdp_delete_data_set
    ADD COLUMN IF NOT EXISTS operator_requested_deletion BOOLEAN NOT NULL DEFAULT FALSE;

-- A row stays until the data set is deleted, also after deletion is requested.
-- It starts nothing: the deletion pipeline reads pdp_delete_data_set only.
CREATE TABLE IF NOT EXISTS pdp_data_set_review (
    data_set BIGINT PRIMARY KEY REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    service_end_epoch BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE pdp_data_set_review IS
    'Data sets whose service was terminated on chain or whose proving stopped, recorded when nobody had requested their deletion. The operator decides what happens to them.';

COMMENT ON COLUMN pdp_data_set_review.service_end_epoch IS
    'FWSS pdpEndEpoch of the data set, NULL until Curio sees the service terminated on chain';

-- Rows nobody requested leave the pipeline unless they already reached the
-- deleteDataSet stage; a failed delete there is retried only after deletion is
-- requested.
DELETE FROM pdp_delete_data_set
WHERE client_requested_termination = FALSE
  AND operator_requested_deletion = FALSE
  AND after_delete_data_set = FALSE
  AND delete_data_set_task_id IS NULL;

INSERT INTO pdp_data_set_review (data_set)
SELECT ds.id
FROM pdp_data_sets ds
WHERE ds.unrecoverable_proving_failure_epoch IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM pdp_delete_data_set dd
      WHERE dd.id = ds.id
        AND (dd.client_requested_termination
             OR dd.operator_requested_deletion
             OR dd.after_delete_data_set
             OR dd.delete_data_set_task_id IS NOT NULL)
  )
ON CONFLICT (data_set) DO NOTHING;
