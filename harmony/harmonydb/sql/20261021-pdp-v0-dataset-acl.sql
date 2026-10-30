-- Cache whether a data set has a FOC keyspace so add/placement can decide
-- without a live chain metadata read. NULL means unresolved.
-- Piece privacy is per-piece (pdp_piecerefs.restricted), not per data set.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND column_name = 'has_keyspace'
    ) THEN
        ALTER TABLE pdp_data_sets
            ADD COLUMN has_keyspace BOOLEAN;
    END IF;
END $$;

COMMENT ON COLUMN pdp_data_sets.has_keyspace IS
    'TRUE when create/chain metadata includes a non-empty foc/ks keyspace id, FALSE once known otherwise, NULL until resolved from create extra_data or chain';

-- Per-piece restricted intent, set at upload finalize. Restricted pieces go to
-- acl-pieces; the data set only needs has_keyspace to accept them.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND column_name = 'restricted'
    ) THEN
        ALTER TABLE pdp_piecerefs
            ADD COLUMN restricted BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

COMMENT ON COLUMN pdp_piecerefs.restricted IS
    'TRUE when upload requested read=restricted; piece is placed in acl-pieces and requires a FEE envelope';

-- Stash restricted on the direct-upload row until the PDP ref is created.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piece_uploads'
          AND column_name = 'restricted'
    ) THEN
        ALTER TABLE pdp_piece_uploads
            ADD COLUMN restricted BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

COMMENT ON COLUMN pdp_piece_uploads.restricted IS
    'Restricted intent from POST /piece read=restricted; copied to pdp_piecerefs.restricted on finalize';
