-- One background sender task per wallet
-- entries created by task_id_upload
CREATE TABLE proofshare_client_sender
(
    wallet_id  BIGINT NOT NULL PRIMARY KEY,
    task_id    BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp
);

ALTER TABLE proofshare_client_requests ADD COLUMN request_type TEXT NOT NULL;

ALTER TABLE proofshare_client_requests DROP CONSTRAINT proofshare_client_requests_pkey;
ALTER TABLE proofshare_client_requests ADD  PRIMARY KEY (sp_id, sector_num, request_type);

ALTER TABLE proofshare_client_requests DROP COLUMN task_id;

ALTER TABLE proofshare_client_requests ADD COLUMN task_id_upload BIGINT;
ALTER TABLE proofshare_client_requests ADD COLUMN task_id_poll   BIGINT;
