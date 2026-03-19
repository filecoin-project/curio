-- fields tracking indexing and ipni jobs over pdp pieces 
ALTER TABLE pdp_piecerefs ADD COLUMN indexing_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN needs_indexing BOOLEAN DEFAULT FALSE;
ALTER TABLE pdp_piecerefs ADD COLUMN ipni_task_id BIGINT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN needs_ipni BOOLEAN DEFAULT FALSE;