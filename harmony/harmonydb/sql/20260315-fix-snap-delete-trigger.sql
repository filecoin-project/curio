-- Fix: The update_sectors_meta_is_cc() trigger function used NEW in the WHERE
-- clause, but NEW is NULL in DELETE triggers. This caused sectors_meta.is_cc to
-- not be reverted to true when a failed snap pipeline entry was removed.

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
    WHERE sp_id = v_sp_id AND sector_num = v_sector_number;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Backfill: correct sectors_meta.is_cc for rows that were affected by this bug.
-- These are sectors where is_cc is false, but the sector is still CC (unsealed CID
-- matches a CC value) and is not currently in the snap pipeline.
UPDATE sectors_meta sm
SET is_cc = true
WHERE sm.is_cc = false
  AND NOT EXISTS (
      SELECT 1 FROM sectors_snap_pipeline snp
      WHERE snp.sp_id = sm.sp_id
        AND snp.sector_number = sm.sector_num
  )
  AND EXISTS (
      SELECT 1 FROM sectors_cc_values ccv
      WHERE ccv.reg_seal_proof = sm.reg_seal_proof
        AND ccv.cur_unsealed_cid = sm.cur_unsealed_cid
  );
