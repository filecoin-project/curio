ALTER TABLE harmony_task ADD COLUMN owner_generation BIGINT NOT NULL DEFAULT 0;

CREATE FUNCTION harmony_task_owner_generation() RETURNS trigger AS $$
BEGIN
    NEW.owner_generation := OLD.owner_generation + 1;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER harmony_task_owner_generation
    BEFORE UPDATE OF owner_id ON harmony_task
    FOR EACH ROW EXECUTE FUNCTION harmony_task_owner_generation();
