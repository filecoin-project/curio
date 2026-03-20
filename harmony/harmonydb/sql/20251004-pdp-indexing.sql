-- fields tracking indexing and ipni jobs over pdp pieces 
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND table_schema = current_schema()
          AND column_name = 'indexing_task_id'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS indexing_task_id BIGINT DEFAULT NULL;
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
          AND column_name = 'needs_indexing'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS needs_indexing BOOLEAN DEFAULT FALSE;
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
          AND column_name = 'ipni_task_id'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS ipni_task_id BIGINT DEFAULT NULL;
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
          AND column_name = 'needs_ipni'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS needs_ipni BOOLEAN DEFAULT FALSE;
    END IF;
END
$$;
