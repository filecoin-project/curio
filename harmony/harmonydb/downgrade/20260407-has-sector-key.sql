-- Downgrade: Restore trigger functions to the 20260315 version that uses
-- orig_sealed_cid = cur_sealed_cid instead of has_sector_key.
-- The has_sector_key column is kept (harmless, avoids data loss).

CREATE OR REPLACE FUNCTION update_is_cc()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.is_cc := (NEW.orig_sealed_cid = NEW.cur_sealed_cid) AND NOT EXISTS (
        SELECT 1
        FROM sectors_snap_pipeline
        WHERE sectors_snap_pipeline.sp_id = NEW.sp_id
          AND sectors_snap_pipeline.sector_number = NEW.sector_num
    ) AND EXISTS (
        SELECT 1
        FROM sectors_cc_values
        WHERE sectors_cc_values.reg_seal_proof = NEW.reg_seal_proof
          AND sectors_cc_values.cur_unsealed_cid = NEW.cur_unsealed_cid
    );

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_sectors_meta_is_cc()
    RETURNS TRIGGER AS $$
DECLARE
    v_sp_id BIGINT;
    v_sector_number BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        v_sp_id := OLD.sp_id;
        v_sector_number := OLD.sector_number;
    ELSE
        v_sp_id := NEW.sp_id;
        v_sector_number := NEW.sector_number;
    END IF;

    UPDATE sectors_meta
    SET is_cc = (sectors_meta.orig_sealed_cid = sectors_meta.cur_sealed_cid) AND NOT EXISTS (
        SELECT 1
        FROM sectors_snap_pipeline
        WHERE sectors_snap_pipeline.sp_id = sectors_meta.sp_id
          AND sectors_snap_pipeline.sector_number = sectors_meta.sector_num
    ) AND EXISTS (
        SELECT 1
        FROM sectors_cc_values
        WHERE sectors_cc_values.reg_seal_proof = sectors_meta.reg_seal_proof
          AND sectors_cc_values.cur_unsealed_cid = sectors_meta.cur_unsealed_cid
    )
    WHERE sp_id = v_sp_id AND sector_num = v_sector_number;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
