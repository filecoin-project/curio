ALTER TABLE message_waits
    ADD COLUMN added_at timestamptz NOT NULL DEFAULT TIMEZONE('UTC', NOW());
