-- Send locks are owned by Harmony tasks and must never outlive their owner.
-- Existing locks without a task cannot be recovered by that task, so remove
-- them before enforcing the ownership invariant.
DELETE FROM message_send_locks msl
WHERE NOT EXISTS (
    SELECT 1 FROM harmony_task ht WHERE ht.id = msl.task_id
);

DELETE FROM message_send_eth_locks msel
WHERE NOT EXISTS (
    SELECT 1 FROM harmony_task ht WHERE ht.id = msel.task_id
);

CREATE INDEX IF NOT EXISTS message_send_locks_task_id_idx
    ON message_send_locks (task_id);

CREATE INDEX IF NOT EXISTS message_send_eth_locks_task_id_idx
    ON message_send_eth_locks (task_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'message_send_locks_task_id_fkey'
          AND conrelid = 'message_send_locks'::regclass
    ) THEN
        ALTER TABLE message_send_locks
            ADD CONSTRAINT message_send_locks_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES harmony_task (id) ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'message_send_eth_locks_task_id_fkey'
          AND conrelid = 'message_send_eth_locks'::regclass
    ) THEN
        ALTER TABLE message_send_eth_locks
            ADD CONSTRAINT message_send_eth_locks_task_id_fkey
            FOREIGN KEY (task_id) REFERENCES harmony_task (id) ON DELETE CASCADE;
    END IF;
END $$;
