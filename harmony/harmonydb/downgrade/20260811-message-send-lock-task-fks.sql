ALTER TABLE message_send_locks
    DROP CONSTRAINT IF EXISTS message_send_locks_task_id_fkey;

ALTER TABLE message_send_eth_locks
    DROP CONSTRAINT IF EXISTS message_send_eth_locks_task_id_fkey;

DROP INDEX IF EXISTS message_send_locks_task_id_idx;
DROP INDEX IF EXISTS message_send_eth_locks_task_id_idx;
