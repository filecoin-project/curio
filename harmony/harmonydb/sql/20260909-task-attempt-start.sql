ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_started_at TIMESTAMPTZ;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_id TEXT;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_start_source TEXT;

COMMENT ON COLUMN harmony_task.attempt_started_at IS 'Current Do-entry timestamp shared with new History records; NULL when not confirmed. Never backfilled.';

CREATE OR REPLACE FUNCTION harmony_task_clear_attempt_start()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.attempt_started_at := NULL;
        NEW.attempt_id := NULL;
        NEW.attempt_start_source := 'claimed';
    ELSIF NEW.owner_id IS NULL OR NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
        NEW.attempt_started_at := NULL;
        NEW.attempt_id := NULL;
        NEW.attempt_start_source := 'claimed';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS harmony_task_clear_attempt_start_trigger ON harmony_task;
CREATE TRIGGER harmony_task_clear_attempt_start_trigger
    BEFORE INSERT OR UPDATE OF owner_id ON harmony_task
    FOR EACH ROW EXECUTE FUNCTION harmony_task_clear_attempt_start();

-- No historical row or timestamp is backfilled. A new same-owner recovery gets
-- a fresh identity from the runner before any new Do invocation.
