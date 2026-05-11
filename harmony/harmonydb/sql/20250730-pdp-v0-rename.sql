-- PDP terminology rename migration
-- Renames: proofset->data_set, proof_set->data_set, root->piece, subroot->sub_piece, sub_root->sub_piece
-- This migration handles database schema changes for PDP terminology to match the current PDPVerifier

-- Check if migration has already been run (idempotency check)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables 
               WHERE table_schema = current_schema() 
               AND table_name = 'pdp_data_sets') THEN
        RAISE NOTICE 'Migration already applied, skipping...';
        RETURN;
    END IF;
END
$$;

-- Step 1: Drop old triggers BEFORE renaming tables (using original table names)
DROP TRIGGER IF EXISTS pdp_proofset_root_insert ON pdp_proofset_roots;
DROP TRIGGER IF EXISTS pdp_proofset_root_delete ON pdp_proofset_roots;
DROP TRIGGER IF EXISTS pdp_proofset_root_update ON pdp_proofset_roots;
DROP TRIGGER IF EXISTS pdp_proofset_create_message_status_change ON message_waits_eth;
DROP TRIGGER IF EXISTS pdp_proofset_add_message_status_change ON message_waits_eth;

-- Step 2: Drop old functions
DROP FUNCTION IF EXISTS increment_proofset_refcount();
DROP FUNCTION IF EXISTS decrement_proofset_refcount();
DROP FUNCTION IF EXISTS adjust_proofset_refcount_on_update();
DROP FUNCTION IF EXISTS update_pdp_proofset_creates();
DROP FUNCTION IF EXISTS update_pdp_proofset_roots();

-- Step 3: Rename tables
ALTER TABLE pdp_proof_sets RENAME TO pdp_data_sets;
ALTER TABLE pdp_proofset_creates RENAME TO pdp_data_set_creates;
ALTER TABLE pdp_proofset_roots RENAME TO pdp_data_set_pieces;
ALTER TABLE pdp_proofset_root_adds RENAME TO pdp_data_set_piece_adds;

-- Step 4: Rename columns in pdp_data_sets (formerly pdp_proof_sets)
-- No column renames needed in this table as it uses 'id' not 'proofset_id'

-- Step 5: Rename columns in pdp_data_set_creates (formerly pdp_proofset_creates)
ALTER TABLE pdp_data_set_creates RENAME COLUMN proofset_created TO data_set_created;

-- Step 6: Rename columns in pdp_data_set_pieces (formerly pdp_proofset_roots)
ALTER TABLE pdp_data_set_pieces RENAME COLUMN proofset TO data_set;
ALTER TABLE pdp_data_set_pieces RENAME COLUMN root TO piece;
ALTER TABLE pdp_data_set_pieces RENAME COLUMN root_id TO piece_id;
ALTER TABLE pdp_data_set_pieces RENAME COLUMN subroot TO sub_piece;
ALTER TABLE pdp_data_set_pieces RENAME COLUMN subroot_offset TO sub_piece_offset;
ALTER TABLE pdp_data_set_pieces RENAME COLUMN subroot_size TO sub_piece_size;

-- Step 7: Rename columns in pdp_data_set_piece_adds (formerly pdp_proofset_root_adds)
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN proofset TO data_set;
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN root TO piece;
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN roots_added TO pieces_added;
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN subroot TO sub_piece;
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN subroot_offset TO sub_piece_offset;
ALTER TABLE pdp_data_set_piece_adds RENAME COLUMN subroot_size TO sub_piece_size;

-- Step 8: Rename columns in pdp_piecerefs
ALTER TABLE pdp_piecerefs RENAME COLUMN proofset_refcount TO data_set_refcount;

-- Step 9: Rename columns in pdp_prove_tasks
ALTER TABLE pdp_prove_tasks RENAME COLUMN proofset TO data_set;

-- Step 10: Rename constraints (PostgreSQL automatically renames constraints when tables are renamed, but let's be explicit)
-- First, get the actual constraint names after table rename
DO $$
DECLARE
    pieces_constraint_name text;
    piece_adds_constraint_name text;
BEGIN
    -- Find the primary key constraint name for pdp_data_set_pieces
    SELECT conname INTO pieces_constraint_name
    FROM pg_constraint
    WHERE conrelid = 'pdp_data_set_pieces'::regclass
    AND contype = 'p';
    
    -- Find the primary key constraint name for pdp_data_set_piece_adds
    SELECT conname INTO piece_adds_constraint_name
    FROM pg_constraint
    WHERE conrelid = 'pdp_data_set_piece_adds'::regclass
    AND contype = 'p';
    
    -- Rename constraints if they still have old names
    IF pieces_constraint_name = 'pdp_proofset_roots_root_id_unique' THEN
        EXECUTE format('ALTER TABLE pdp_data_set_pieces RENAME CONSTRAINT %I TO pdp_data_set_pieces_piece_id_unique', pieces_constraint_name);
    END IF;
    
    IF piece_adds_constraint_name = 'pdp_proofset_root_adds_root_id_unique' THEN
        EXECUTE format('ALTER TABLE pdp_data_set_piece_adds RENAME CONSTRAINT %I TO pdp_data_set_piece_adds_piece_id_unique', piece_adds_constraint_name);
    END IF;
END
$$;

-- Step 11: Create new functions with updated column names
CREATE OR REPLACE FUNCTION increment_data_set_refcount()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET data_set_refcount = data_set_refcount + 1
    WHERE id = NEW.pdp_pieceref;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION decrement_data_set_refcount()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET data_set_refcount = data_set_refcount - 1
    WHERE id = OLD.pdp_pieceref;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION adjust_data_set_refcount_on_update()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.pdp_pieceref IS DISTINCT FROM NEW.pdp_pieceref THEN
        -- Decrement count for old reference if not null
        IF OLD.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET data_set_refcount = data_set_refcount - 1
            WHERE id = OLD.pdp_pieceref;
        END IF;
        -- Increment count for new reference if not null
        IF NEW.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET data_set_refcount = data_set_refcount + 1
            WHERE id = NEW.pdp_pieceref;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Step 12: Create message status change functions
CREATE OR REPLACE FUNCTION update_pdp_data_set_creates()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        -- Update the ok field in pdp_data_set_creates if a matching entry exists
        UPDATE pdp_data_set_creates
        SET ok = CASE
                     WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                     WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                     ELSE ok
            END
        WHERE create_message_hash = NEW.signed_tx_hash AND data_set_created = FALSE;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_pdp_data_set_piece_adds()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        -- Update the add_message_ok field in pdp_data_set_piece_adds if a matching entry exists
        UPDATE pdp_data_set_piece_adds
        SET add_message_ok = CASE
                                WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                                WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                                ELSE add_message_ok
                            END
        WHERE add_message_hash = NEW.signed_tx_hash;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Step 13: Create new triggers with updated names
CREATE TRIGGER pdp_data_set_piece_insert
    AFTER INSERT ON pdp_data_set_pieces
    FOR EACH ROW
    WHEN (NEW.pdp_pieceref IS NOT NULL)
EXECUTE FUNCTION increment_data_set_refcount();

CREATE TRIGGER pdp_data_set_piece_delete
    AFTER DELETE ON pdp_data_set_pieces
    FOR EACH ROW
    WHEN (OLD.pdp_pieceref IS NOT NULL)
EXECUTE FUNCTION decrement_data_set_refcount();

CREATE TRIGGER pdp_data_set_piece_update
    AFTER UPDATE ON pdp_data_set_pieces
    FOR EACH ROW
EXECUTE FUNCTION adjust_data_set_refcount_on_update();

CREATE TRIGGER pdp_data_set_create_message_status_change
    AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
    FOR EACH ROW
EXECUTE PROCEDURE update_pdp_data_set_creates();

CREATE TRIGGER pdp_data_set_add_message_status_change
    AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
    FOR EACH ROW
EXECUTE PROCEDURE update_pdp_data_set_piece_adds();

-- Step 14: Update indexes
-- Handle the index that was added in a later migration
DO $$
BEGIN
    -- Check if the old index exists and rename it
    IF EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'idx_pdp_proofset_root_adds_roots_added') THEN
        ALTER INDEX idx_pdp_proofset_root_adds_roots_added RENAME TO idx_pdp_data_set_piece_adds_pieces_added;
    END IF;
END
$$;

-- Step 15: Update foreign key constraints
-- PostgreSQL automatically updates foreign key references when tables are renamed,
-- but let's add a verification step
DO $$
DECLARE
    fk_count integer;
BEGIN
    -- Verify that foreign keys were updated correctly
    SELECT COUNT(*) INTO fk_count
    FROM information_schema.table_constraints tc
    JOIN information_schema.constraint_column_usage ccu
        ON tc.constraint_name = ccu.constraint_name
        AND tc.constraint_schema = ccu.constraint_schema
    WHERE tc.constraint_type = 'FOREIGN KEY'
        AND tc.table_schema = current_schema()
        AND ccu.table_name IN ('pdp_data_sets', 'pdp_data_set_creates', 
                               'pdp_data_set_pieces', 'pdp_data_set_piece_adds');
    
    IF fk_count = 0 THEN
        RAISE WARNING 'No foreign key constraints found - this may be expected depending on schema';
    END IF;
END
$$;

-- Step 17: Final verification
DO $$
DECLARE
    table_count integer;
    column_count integer;
BEGIN
    -- Verify all tables were renamed
    SELECT COUNT(*) INTO table_count
    FROM information_schema.tables
    WHERE table_schema = current_schema()
    AND table_name IN ('pdp_data_sets', 'pdp_data_set_creates', 
                       'pdp_data_set_pieces', 'pdp_data_set_piece_adds');
    
    IF table_count != 4 THEN
        RAISE EXCEPTION 'Not all tables were renamed successfully';
    END IF;
    
    -- Verify critical columns were renamed
    SELECT COUNT(*) INTO column_count
    FROM information_schema.columns
    WHERE table_schema = current_schema()
    AND (
        (table_name = 'pdp_data_set_creates' AND column_name = 'data_set_created') OR
        (table_name = 'pdp_data_set_pieces' AND column_name = 'data_set') OR
        (table_name = 'pdp_data_set_pieces' AND column_name = 'piece_id') OR
        (table_name = 'pdp_piecerefs' AND column_name = 'data_set_refcount')
    );
    
    IF column_count != 4 THEN
        RAISE EXCEPTION 'Not all columns were renamed successfully';
    END IF;
    
    RAISE NOTICE 'Migration completed successfully!';
END
$$;