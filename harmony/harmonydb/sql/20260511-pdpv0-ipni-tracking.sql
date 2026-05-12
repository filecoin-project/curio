DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND table_schema = current_schema()
          AND column_name = 'indexed_at'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS indexed_at TIMESTAMPTZ DEFAULT NULL;
    END IF;
END
$$;


DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND table_schema = current_schema()
          AND column_name = 'advertisement_created_at'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS advertisement_created_at TIMESTAMPTZ DEFAULT NULL;
    END IF;
END
$$;

UPDATE pdp_piecerefs SET indexed_at = now(); -- This will create temporary inconsistency for rows which are not processed which will be resolved when they are processed.
UPDATE pdp_piecerefs SET advertisement_created_at = now(); -- This will create temporary inconsistency for rows which are not processed which will be resolved when they are processed.
