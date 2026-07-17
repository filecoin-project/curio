-- pdp_data_set_piece_adds holds one row per SUB-piece, but the primary key
-- (add_message_hash, add_message_index) only distinguishes pieces: every
-- sub-piece row of an aggregated piece shares the piece's add_message_index,
-- so adding a piece with two or more sub-pieces violates the PK on the second
-- row. The handler has already broadcast the AddPieces transaction at that
-- point, so the insert failure strands an on-chain piece with no local
-- tracking (never proven, never indexed).
--
-- The pre-20251010 key (data_set, add_message_hash, sub_piece_offset) had the
-- mirror-image flaw: single-sub-piece pieces in a multi-piece batch all sit at
-- offset 0 and collided. Row identity needs BOTH discriminators. data_set
-- stays out of the key: it is nullable for the create-and-add flow (20251015).
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
    PRIMARY KEY (add_message_hash, add_message_index, sub_piece_offset);
END $$;
