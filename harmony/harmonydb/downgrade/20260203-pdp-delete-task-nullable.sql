-- WARNING: will fail if any rows have NULL delete_data_set_task_id
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'pdp_delete_data_set'
      AND column_name = 'delete_data_set_task_id'
      AND is_nullable = 'YES'
  ) THEN
    ALTER TABLE pdp_delete_data_set
      ALTER COLUMN delete_data_set_task_id SET NOT NULL;
  END IF;
END $$;
