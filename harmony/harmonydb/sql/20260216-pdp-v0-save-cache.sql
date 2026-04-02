-- All columns marked for cache saving by default,
-- `task_save_cache` will flip it to FALSE if piece size is too small for caching
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND table_schema = current_schema()
          AND column_name = 'needs_save_cache'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS needs_save_cache BOOLEAN DEFAULT TRUE;
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
          AND column_name = 'save_cache_task_id'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS save_cache_task_id BIGINT DEFAULT NULL;
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
          AND column_name = 'caching_task_started'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS caching_task_started TIMESTAMP WITH TIME ZONE DEFAULT NULL;
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
          AND column_name = 'caching_task_completed'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS caching_task_completed TIMESTAMP WITH TIME ZONE DEFAULT NULL;
    END IF;
END
$$;

-- Track consecutive cached proofgen failures, due to cache misses or otherwise, strictly for monitoring
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_piecerefs'
          AND table_schema = current_schema()
          AND column_name = 'cached_proofgen_failure_count'
    ) THEN
        ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS cached_proofgen_failure_count INTEGER DEFAULT 0;
    END IF;
END
$$;
