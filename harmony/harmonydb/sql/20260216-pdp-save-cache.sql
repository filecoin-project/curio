-- All columns marked for cache saving by default,
-- `task_save_cache` will flip it to FALSE if piece size is too small for caching
ALTER TABLE pdp_piecerefs ADD COLUMN needs_save_cache BOOLEAN DEFAULT TRUE;

ALTER TABLE pdp_piecerefs ADD COLUMN save_cache_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_started TIMESTAMP WITH TIME ZONE DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_completed TIMESTAMP WITH TIME ZONE DEFAULT NULL;

-- Track consecutive cached proofgen failures, due to cache misses or otherwise, strictly for monitoring
ALTER TABLE pdp_piecerefs ADD COLUMN cached_proofgen_failure_count INTEGER DEFAULT 0;
