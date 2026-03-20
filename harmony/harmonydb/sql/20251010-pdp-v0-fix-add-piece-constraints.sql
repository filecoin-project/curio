-- changes an errand constraint from (data_set, add_message_hash, sub_piece_offset) to (data_set, add_message_hash, add_message_index)

ALTER TABLE pdp_data_set_piece_adds
  DROP CONSTRAINT IF EXISTS pdp_data_set_piece_adds_piece_id_unique;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_pk
      PRIMARY KEY (data_set HASH, add_message_hash ASC, add_message_index ASC);
  END IF;
END $$;
