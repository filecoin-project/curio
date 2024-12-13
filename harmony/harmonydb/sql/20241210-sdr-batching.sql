ALTER TABLE sectors_sdr_pipeline ADD COLUMN precommit_ready_at TIMESTAMPTZ;
ALTER TABLE sectors_sdr_pipeline ADD COLUMN commit_ready_at TIMESTAMPTZ;

-- Function to precommit_ready_at value. Used by the trigger
CREATE OR REPLACE FUNCTION set_precommit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_tree_r column is changing from FALSE to TRUE
    IF OLD.after_tree_r = FALSE AND NEW.after_tree_r = TRUE THEN
        -- Explicitly set precommit_ready_at to the current UTC timestamp
        UPDATE sectors_sdr_pipeline SET precommit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;

    -- Return the modified row
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


-- Function to set commit_ready_at. Used by trigger
CREATE OR REPLACE FUNCTION set_commit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_porep column is changing from FALSE to TRUE
    IF OLD.after_porep = FALSE AND NEW.after_porep = TRUE THEN
       -- Explicitly set precommit_ready_at to the current UTC timestamp
        UPDATE sectors_sdr_pipeline SET commit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;

    -- Return the modified row
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_precommit_ready_at
    AFTER INSERT OR UPDATE OR DELETE ON sectors_sdr_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_precommit_ready_at();


CREATE TRIGGER update_commit_ready_at
    AFTER INSERT OR UPDATE OR DELETE ON sectors_sdr_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_commit_ready_at();

ALTER TABLE sectors_snap_pipeline ADD COLUMN update_ready_at TIMESTAMPTZ;

-- Function to precommit_ready_at value. Used by the trigger
CREATE OR REPLACE FUNCTION set_update_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if after_prove column is changing from FALSE to TRUE
    IF OLD.after_prove = FALSE AND NEW.after_prove = TRUE THEN
        -- Explicitly set update_ready_at to the current UTC timestamp
        UPDATE sectors_snap_pipeline SET update_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;

    -- Return the modified row
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_update_ready_at
    AFTER INSERT OR UPDATE OR DELETE ON sectors_snap_pipeline
    FOR EACH ROW EXECUTE FUNCTION set_update_ready_at();
