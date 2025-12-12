-- One background sender task per wallet
-- entries created by task_id_upload
CREATE TABLE IF NOT EXISTS proofshare_client_sender
(
    wallet_id  BIGINT NOT NULL PRIMARY KEY,
    task_id    BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp
);

ALTER TABLE proofshare_client_requests ADD COLUMN IF NOT EXISTS request_type TEXT NOT NULL DEFAULT 'porep';

ALTER TABLE proofshare_client_requests DROP CONSTRAINT IF EXISTS proofshare_client_requests_pkey;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE table_name = 'proofshare_client_requests' 
        AND constraint_type = 'PRIMARY KEY'
    ) THEN
        ALTER TABLE proofshare_client_requests ADD PRIMARY KEY (sp_id, sector_num, request_type);
    END IF;
END $$;

ALTER TABLE proofshare_client_requests DROP COLUMN IF EXISTS task_id;

ALTER TABLE proofshare_client_requests ADD COLUMN IF NOT EXISTS task_id_upload BIGINT;
ALTER TABLE proofshare_client_requests ADD COLUMN IF NOT EXISTS task_id_poll   BIGINT;
