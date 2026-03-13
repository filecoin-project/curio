ALTER TABLE balance_manager_addresses DROP CONSTRAINT IF EXISTS subject_not_equal_second;
ALTER TABLE balance_manager_addresses DROP COLUMN IF EXISTS subject_type;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'subject_not_equal_second'
      AND conrelid = 'balance_manager_addresses'::regclass
  ) THEN
    ALTER TABLE balance_manager_addresses
      ADD CONSTRAINT subject_not_equal_second CHECK (subject_address != second_address);
  END IF;
END $$;
