-- Restore old PK: (data_set HASH, add_message_hash ASC, add_message_index ASC) and make data_set NOT NULL
-- WARNING: will fail if any rows have NULL data_set values
DO $$
BEGIN
  -- Drop new PK if it exists
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      DROP CONSTRAINT pdp_data_set_piece_adds_pk;
  END IF;

  -- Make data_set NOT NULL again
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'pdp_data_set_piece_adds'
      AND column_name = 'data_set'
      AND is_nullable = 'YES'
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ALTER COLUMN data_set SET NOT NULL;
  END IF;

  -- Restore old PK
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_pk
      PRIMARY KEY (data_set HASH, add_message_hash ASC, add_message_index ASC);
  END IF;
END $$;
