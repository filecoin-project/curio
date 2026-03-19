-- Migration: Rename and update foreign keys on pdp_data_set_piece_adds and pdp_data_set_pieces
-- Changes: Renames old *_pdp_pieceref_fkey constraints and sets them to ON DELETE CASCADE
-- 1. Drop the existing foreign key constraint if it exists
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_proofset_root_adds_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_piece_adds'
    ) THEN
        ALTER TABLE pdp_data_set_piece_adds
            DROP CONSTRAINT pdp_proofset_root_adds_pdp_pieceref_fkey;
    END IF;
END $$;

-- 2. Create the new foreign key constraint with correct name and ON DELETE behavior
ALTER TABLE pdp_data_set_piece_adds
    ADD CONSTRAINT pdp_data_set_piece_adds_pdp_pieceref_fkey
    FOREIGN KEY (pdp_pieceref)
    REFERENCES pdp_piecerefs(id)
    ON DELETE CASCADE;


-- ===== MODIFY pdp_data_set_pieces FOREIGN KEY =====

-- 3. Drop the existing foreign key constraint if it exists
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_name = 'pdp_proofset_roots_pdp_pieceref_fkey'
          AND table_name = 'pdp_data_set_pieces'
    ) THEN
        ALTER TABLE pdp_data_set_pieces
            DROP CONSTRAINT pdp_proofset_roots_pdp_pieceref_fkey;
    END IF;
END $$;

-- 4. Create the new foreign key constraint with correct name and ON DELETE behavior
ALTER TABLE pdp_data_set_pieces
    ADD CONSTRAINT pdp_data_set_pieces_pdp_pieceref_fkey
    FOREIGN KEY (pdp_pieceref)
    REFERENCES pdp_piecerefs(id)
    ON DELETE CASCADE;

COMMIT;
