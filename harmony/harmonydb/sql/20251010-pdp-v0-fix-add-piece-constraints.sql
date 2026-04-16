-- This file was update on 16th April 2026 to change the primary key from Yugabyte specific to Postgres style.
-- Any SP, which has already run this file, will never run again. So, new file 20260414-pdp-v0-fix-add-piece-constraints.sql
-- will fix the constraint for them if required. New SPs will get the correct constraint from here.
-- Note: This goes against best practices of never changing the already executed SQL files. This is the only exception.


-- changes an errand constraint from (data_set, add_message_hash, sub_piece_offset) to (data_set, add_message_hash, add_message_index)

ALTER TABLE pdp_data_set_piece_adds
  DROP CONSTRAINT IF EXISTS pdp_data_set_piece_adds_piece_id_unique;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
WHERE conname = 'pdp_data_set_piece_adds_pk'``
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_pk
      PRIMARY KEY (data_set, add_message_hash, add_message_index);
  END IF;
END $$;