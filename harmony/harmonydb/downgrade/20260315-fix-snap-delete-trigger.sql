-- Downgrade: Restore original trigger functions without orig_sealed_cid check
-- and with the DELETE trigger bug (uses NEW instead of OLD)

CREATE OR REPLACE FUNCTION update_is_cc()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.is_cc := NOT EXISTS (
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
BEGIN
    UPDATE sectors_meta
    SET is_cc = NOT EXISTS (
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
    WHERE sp_id = NEW.sp_id AND sector_num = NEW.sector_number;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
