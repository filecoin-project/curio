ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS indexing_task_id;
ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS needs_indexing;
ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS ipni_task_id;
ALTER TABLE pdp_piecerefs DROP COLUMN IF EXISTS needs_ipni;
