-- fields tracking indexing and ipni jobs over pdp pieces 
ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS indexing_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS needs_indexing BOOLEAN DEFAULT FALSE;
ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS ipni_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS needs_ipni BOOLEAN DEFAULT FALSE;