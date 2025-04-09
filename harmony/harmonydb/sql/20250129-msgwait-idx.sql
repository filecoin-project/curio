-- Index for UPDATE message_waits SET waiter_machine_id = .. WHERE waiter_machine_id IS NULL AND executed_tsk_cid IS NULL

CREATE INDEX IF NOT EXISTS idx_message_waits_nulls
    ON message_waits (waiter_machine_id)
    WHERE waiter_machine_id IS NULL AND executed_tsk_cid IS NULL;

