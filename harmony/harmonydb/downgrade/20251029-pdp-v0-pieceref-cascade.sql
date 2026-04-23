-- Revert ON DELETE CASCADE back to ON DELETE SET NULL for pdp_pieceref FKs.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'pdp_data_set_piece_adds_pdp_pieceref_fkey'
          AND conrelid = 'pdp_data_set_piece_adds'::regclass
          AND confdeltype = 'c'
    ) THEN
        ALTER TABLE pdp_data_set_piece_adds
            DROP CONSTRAINT pdp_data_set_piece_adds_pdp_pieceref_fkey;
        ALTER TABLE pdp_data_set_piece_adds
            ADD CONSTRAINT pdp_data_set_piece_adds_pdp_pieceref_fkey
            FOREIGN KEY (pdp_pieceref)
            REFERENCES pdp_piecerefs(id)
            ON DELETE SET NULL;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'pdp_data_set_pieces_pdp_pieceref_fkey'
          AND conrelid = 'pdp_data_set_pieces'::regclass
          AND confdeltype = 'c'
    ) THEN
        ALTER TABLE pdp_data_set_pieces
            DROP CONSTRAINT pdp_data_set_pieces_pdp_pieceref_fkey;
        ALTER TABLE pdp_data_set_pieces
            ADD CONSTRAINT pdp_data_set_pieces_pdp_pieceref_fkey
            FOREIGN KEY (pdp_pieceref)
            REFERENCES pdp_piecerefs(id)
            ON DELETE SET NULL;
    END IF;
END
$$;
