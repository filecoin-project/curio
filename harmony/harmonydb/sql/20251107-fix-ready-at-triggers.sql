-- Drop the old triggers
DROP TRIGGER IF EXISTS update_precommit_ready_at ON sectors_sdr_pipeline;
DROP TRIGGER IF EXISTS update_commit_ready_at ON sectors_sdr_pipeline;
DROP TRIGGER IF EXISTS update_update_ready_at ON sectors_snap_pipeline;

-- Recreate the functions to use BEFORE triggers and directly modify NEW
CREATE OR REPLACE FUNCTION set_precommit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_tree_r column is changing from FALSE to TRUE
    IF (TG_OP = 'INSERT' OR OLD.after_tree_r = FALSE) AND NEW.after_tree_r = TRUE AND NEW.precommit_ready_at IS NULL THEN
        NEW.precommit_ready_at := CURRENT_TIMESTAMP AT TIME ZONE 'UTC';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_commit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_porep column is changing from FALSE to TRUE
    IF (TG_OP = 'INSERT' OR OLD.after_porep = FALSE) AND NEW.after_porep = TRUE AND NEW.commit_ready_at IS NULL THEN
        NEW.commit_ready_at := CURRENT_TIMESTAMP AT TIME ZONE 'UTC';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_update_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_prove column is changing from FALSE to TRUE
    IF (TG_OP = 'INSERT' OR OLD.after_prove = FALSE) AND NEW.after_prove = TRUE AND NEW.update_ready_at IS NULL THEN
        NEW.update_ready_at := CURRENT_TIMESTAMP AT TIME ZONE 'UTC';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create BEFORE triggers instead of AFTER triggers
CREATE TRIGGER update_precommit_ready_at 
    BEFORE INSERT OR UPDATE ON sectors_sdr_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_precommit_ready_at();

CREATE TRIGGER update_commit_ready_at 
    BEFORE INSERT OR UPDATE ON sectors_sdr_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_commit_ready_at();

CREATE TRIGGER update_update_ready_at 
    BEFORE INSERT OR UPDATE ON sectors_snap_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_update_ready_at();

-- Backfill existing rows that are missing these timestamps
UPDATE sectors_sdr_pipeline 
SET precommit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC' 
WHERE after_tree_r = TRUE AND precommit_ready_at IS NULL;

UPDATE sectors_sdr_pipeline 
SET commit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC' 
WHERE after_porep = TRUE AND commit_ready_at IS NULL;

UPDATE sectors_snap_pipeline 
SET update_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC' 
WHERE after_prove = TRUE AND update_ready_at IS NULL;

