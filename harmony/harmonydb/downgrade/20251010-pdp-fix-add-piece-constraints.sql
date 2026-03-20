-- Restore old PK: (data_set HASH, add_message_hash ASC, sub_piece_offset ASC)
-- WARNING: will fail if rows violate the old uniqueness constraint
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      DROP CONSTRAINT pdp_data_set_piece_adds_pk;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_piece_id_unique'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_piece_id_unique
      PRIMARY KEY (data_set HASH, add_message_hash ASC, sub_piece_offset ASC);
  END IF;
END $$;
