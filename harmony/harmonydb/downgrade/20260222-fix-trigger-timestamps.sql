-- Downgrade: Restore trigger functions that use CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
-- (the buggy form, for rollback purposes only)

CREATE OR REPLACE FUNCTION set_precommit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_tree_r = FALSE AND NEW.after_tree_r = TRUE THEN
        UPDATE sectors_sdr_pipeline SET precommit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_commit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_porep = FALSE AND NEW.after_porep = TRUE THEN
        UPDATE sectors_sdr_pipeline SET commit_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_update_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_prove = FALSE AND NEW.after_prove = TRUE THEN
        UPDATE sectors_snap_pipeline SET update_ready_at = CURRENT_TIMESTAMP AT TIME ZONE 'UTC'
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
