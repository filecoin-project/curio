ALTER TABLE message_waits ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT TIMEZONE('UTC', NOW());

CREATE INDEX IF NOT EXISTS idx_message_waits_created_at_executed
    ON message_waits (created_at)
    WHERE executed_tsk_cid IS NOT NULL;

