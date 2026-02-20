-- Add save cache task columns to pdp_piecerefs (mirrors indexing pattern)
ALTER TABLE pdp_piecerefs ADD COLUMN needs_save_cache BOOLEAN DEFAULT TRUE;
ALTER TABLE pdp_piecerefs ADD COLUMN save_cache_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_started TIMESTAMP WITH TIME ZONE DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_completed TIMESTAMP WITH TIME ZONE DEFAULT NULL;
