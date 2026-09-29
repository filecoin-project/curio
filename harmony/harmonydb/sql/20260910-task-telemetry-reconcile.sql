-- The runner records only the first eight filename characters. Either of the
-- two 20260909 files may already be recorded while the other was skipped.
-- Use a new key to reconcile both definitions without rewriting that history.
-- DDL only: preserve owners, task state, attempt tokens and valid timestamps.
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_started_at TIMESTAMPTZ;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_id TEXT;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS attempt_start_source TEXT;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS work_start TIMESTAMPTZ;
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS work_start_source TEXT;

COMMENT ON COLUMN harmony_task.attempt_started_at IS 'Current Do-entry timestamp shared with new History records; NULL when not confirmed. Never backfilled.';
COMMENT ON COLUMN harmony_task.work_start IS 'Current ownership timestamp, not task execution start; trusted only with work_start_source.';
COMMENT ON COLUMN harmony_task.work_start_source IS 'claim for newly observed ownership; NULL means unknown legacy/migration provenance.';

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

CREATE OR REPLACE FUNCTION harmony_task_sync_work_start()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.owner_id IS NULL THEN
        NEW.work_start := NULL;
        NEW.work_start_source := NULL;
    ELSIF TG_OP = 'INSERT' THEN
        NEW.work_start := CURRENT_TIMESTAMP;
        NEW.work_start_source := 'claim';
    ELSIF NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
        NEW.work_start := CURRENT_TIMESTAMP;
        NEW.work_start_source := 'claim';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS harmony_task_sync_work_start_trigger ON harmony_task;
CREATE TRIGGER harmony_task_sync_work_start_trigger
    BEFORE INSERT OR UPDATE OF owner_id ON harmony_task
    FOR EACH ROW EXECUTE FUNCTION harmony_task_sync_work_start();
