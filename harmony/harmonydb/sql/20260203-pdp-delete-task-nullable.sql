-- Fix: make delete_data_set_task_id nullable in pdp_delete_data_set
-- The task code sets this column to NULL when the task completes, and queries for IS NULL
-- to find pending work, but the schema incorrectly enforced NOT NULL.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_name = 'pdp_delete_data_set'
      AND column_name = 'delete_data_set_task_id'
      AND is_nullable = 'NO'
  ) THEN
    ALTER TABLE pdp_delete_data_set
      ALTER COLUMN delete_data_set_task_id DROP NOT NULL;
  END IF;
END $$;
