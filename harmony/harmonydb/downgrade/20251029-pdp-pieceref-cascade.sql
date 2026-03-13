-- Restore old FK names with ON DELETE SET NULL

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_data_set_piece_adds_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_piece_adds'
    ) THEN
        ALTER TABLE pdp_data_set_piece_adds
            DROP CONSTRAINT pdp_data_set_piece_adds_pdp_pieceref_fkey;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_proofset_root_adds_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_piece_adds'
    ) THEN
        ALTER TABLE pdp_data_set_piece_adds
            ADD CONSTRAINT pdp_proofset_root_adds_pdp_pieceref_fkey
            FOREIGN KEY (pdp_pieceref)
            REFERENCES pdp_piecerefs(id)
            ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_data_set_pieces_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_pieces'
    ) THEN
        ALTER TABLE pdp_data_set_pieces
            DROP CONSTRAINT pdp_data_set_pieces_pdp_pieceref_fkey;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_proofset_roots_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_pieces'
    ) THEN
        ALTER TABLE pdp_data_set_pieces
            ADD CONSTRAINT pdp_proofset_roots_pdp_pieceref_fkey
            FOREIGN KEY (pdp_pieceref)
            REFERENCES pdp_piecerefs(id)
            ON DELETE SET NULL;
    END IF;
END $$;
