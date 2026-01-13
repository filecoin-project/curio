CREATE TABLE IF NOT EXISTS sectors_unseal_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    reg_seal_proof BIGINT NOT NULL,

    create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,

    task_id_unseal_sdr BIGINT, -- builds unseal cache
    after_unseal_sdr bool NOT NULL DEFAULT FALSE,

    task_id_decode_sector BIGINT, -- makes the "unsealed" copy (runs in target long-term storage)
    after_decode_sector bool NOT NULL DEFAULT FALSE,

    primary key (sp_id, sector_number)
);

ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS target_unseal_state BOOLEAN;

-- To unseal
-- 1. Target unseal state is true
-- 2. No unsealed sector entry in sector_location
-- 3. No unsealed sector entry in sectors_unseal_pipeline

CREATE OR REPLACE FUNCTION update_sectors_unseal_pipeline_materialized(
    target_sp_id BIGINT,
    target_sector_num BIGINT
) RETURNS VOID AS $$
DECLARE
    should_be_added BOOLEAN;
    should_not_be_removed BOOLEAN;
BEGIN
    -- Check if the sector should be in the materialized table
    SELECT EXISTS (
        SELECT 1
        FROM sectors_meta sm
        WHERE sm.sp_id = target_sp_id
          AND sm.sector_num = target_sector_num
          AND sm.target_unseal_state = TRUE
          AND sm.is_cc = FALSE
          AND NOT EXISTS (
            SELECT 1 FROM sector_location sl
            WHERE sl.miner_id = sm.sp_id
              AND sl.sector_num = sm.sector_num
              AND sl.sector_filetype = 1 -- 1 is unsealed
        )
          AND NOT EXISTS (
            SELECT 1 FROM sectors_unseal_pipeline sup
            WHERE sup.sp_id = sm.sp_id
              AND sup.sector_number = sm.sector_num
        )
    ) INTO should_be_added;

    -- If it should be in the materialized table
    IF should_be_added THEN
        -- Insert or update the row
        INSERT INTO sectors_unseal_pipeline (sp_id, sector_number, reg_seal_proof)
            SELECT sm.sp_id, sm.sector_num, sm.reg_seal_proof
            FROM sectors_meta sm
            WHERE sm.sp_id = target_sp_id AND sm.sector_num = target_sector_num
        ON CONFLICT (sp_id, sector_number) DO UPDATE
            SET reg_seal_proof = EXCLUDED.reg_seal_proof;
    -- no else, the pipeline entries remove themselves after the unseal is done
    END IF;

    -- Check if the sector should not be removed
    SELECT EXISTS (
        SELECT 1
        FROM sectors_meta sm
        WHERE sm.sp_id = target_sp_id
          AND sm.sector_num = target_sector_num
          AND sm.target_unseal_state = TRUE
    ) INTO should_not_be_removed;

    -- If it should not be removed
    IF should_not_be_removed THEN
        -- Just in case make sure the sector is not scheduled for removal
        DELETE FROM storage_removal_marks
            WHERE sp_id = target_sp_id AND sector_num = target_sector_num AND sector_filetype = 1; -- 1 is unsealed
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION trig_sectors_meta_update_materialized() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' OR TG_OP = 'UPDATE' THEN
        PERFORM update_sectors_unseal_pipeline_materialized(NEW.sp_id, NEW.sector_num);
    ELSIF TG_OP = 'DELETE' THEN
        PERFORM update_sectors_unseal_pipeline_materialized(OLD.sp_id, OLD.sector_num);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'trig_sectors_meta_update_materialized'
    ) THEN
        CREATE TRIGGER trig_sectors_meta_update_materialized AFTER INSERT OR UPDATE OR DELETE ON sectors_meta
    FOR EACH ROW EXECUTE FUNCTION trig_sectors_meta_update_materialized();
    END IF;
END $$;

-- not triggering on sector_location, storage can be detached occasionally and auto-scheduling 10000s of unseals is bad
