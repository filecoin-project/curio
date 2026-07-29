ALTER TABLE message_sends_eth
ALTER COLUMN send_time TYPE TIMESTAMP
    USING send_time AT TIME ZONE current_setting('TimeZone');

ALTER TABLE message_send_eth_locks
ALTER COLUMN claimed_at TYPE TIMESTAMP
    USING claimed_at AT TIME ZONE current_setting('TimeZone');
