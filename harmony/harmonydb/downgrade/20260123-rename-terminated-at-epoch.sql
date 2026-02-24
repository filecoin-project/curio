DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'pdp_data_sets'
      AND column_name = 'unrecoverable_proving_failure_epoch'
  ) THEN
    ALTER TABLE pdp_data_sets RENAME COLUMN unrecoverable_proving_failure_epoch TO terminated_at_epoch;
  END IF;
END $$;

COMMENT ON COLUMN pdp_data_sets.terminated_at_epoch IS 'Block height at which dataset termination was detected; NULL if active';
