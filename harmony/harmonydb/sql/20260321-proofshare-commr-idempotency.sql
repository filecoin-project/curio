-- Add sealed_cid (CommR) to proofshare_client_requests idempotency key.
-- This prevents stale proof data from being reused when a sector is retried with different data
-- (e.g. snapdeal retry with new CommR).

ALTER TABLE proofshare_client_requests ADD COLUMN IF NOT EXISTS sealed_cid TEXT NOT NULL DEFAULT '';

-- Update primary key to include sealed_cid
ALTER TABLE proofshare_client_requests DROP CONSTRAINT IF EXISTS proofshare_client_requests_pkey;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE table_name = 'proofshare_client_requests'
        AND constraint_type = 'PRIMARY KEY'
    ) THEN
        ALTER TABLE proofshare_client_requests ADD PRIMARY KEY (sp_id, sector_num, request_type, sealed_cid);
    END IF;
END $$;
