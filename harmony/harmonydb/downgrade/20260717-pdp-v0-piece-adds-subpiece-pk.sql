-- Revert the piece_adds primary key to (add_message_hash, add_message_index).
-- Note: this fails if the table holds a piece with multiple sub-pieces (rows
-- that are only distinguished by sub_piece_offset); remove those rows first.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = to_regclass('pdp_data_set_piece_adds')
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      DROP CONSTRAINT pdp_data_set_piece_adds_pk;
  END IF;

  ALTER TABLE pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_data_set_piece_adds_pk
    PRIMARY KEY (add_message_hash, add_message_index);
END $$;
