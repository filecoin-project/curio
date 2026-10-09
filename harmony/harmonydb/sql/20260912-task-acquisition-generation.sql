-- Fence asynchronous admission cleanup, including failures before an attempt
-- token was installed. Existing in-flight provenance is deliberately untouched.
ALTER TABLE harmony_task ADD COLUMN IF NOT EXISTS owner_generation BIGINT NOT NULL DEFAULT 0;

CREATE OR REPLACE FUNCTION harmony_task_acquisition_generation()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.owner_id IS DISTINCT FROM OLD.owner_id THEN
        NEW.owner_generation := OLD.owner_generation + 1;
    ELSIF NEW.owner_generation IS DISTINCT FROM OLD.owner_generation THEN
        -- Recovery is a new acquisition even when the machine ID is unchanged.
        NEW.owner_generation := OLD.owner_generation + 1;
        NEW.attempt_id := NULL;
        NEW.attempt_started_at := NULL;
        NEW.attempt_start_source := 'claimed';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS harmony_task_acquisition_generation ON harmony_task;
CREATE TRIGGER harmony_task_acquisition_generation
BEFORE UPDATE OF owner_id, owner_generation ON harmony_task
FOR EACH ROW EXECUTE FUNCTION harmony_task_acquisition_generation();
