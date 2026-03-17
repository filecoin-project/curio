-- Fix 1: The update_sectors_meta_is_cc() trigger function used NEW in the WHERE
-- clause, but NEW is NULL in DELETE triggers. This caused sectors_meta.is_cc to
-- not be reverted to true when a failed snap pipeline entry was removed.
--
-- Fix 2: The is_cc computation did not check orig_sealed_cid = cur_sealed_cid.
-- A sector that has been snapped (orig_sealed_cid != cur_sealed_cid) can never
-- be CC again — snap deals cannot be re-upgraded. Adding this check makes is_cc
-- strictly more correct regardless of cur_unsealed_cid state.

-- Replace the BEFORE INSERT/UPDATE trigger on sectors_meta
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

-- Replace the AFTER INSERT/UPDATE/DELETE trigger on sectors_snap_pipeline
-- Fix: use OLD for DELETE triggers (NEW is NULL), add orig_sealed_cid check
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

-- Backfill 1: Sectors incorrectly marked is_cc=false that are actually CC
-- (failed snap removed from pipeline, but is_cc not reverted due to DELETE trigger bug)
UPDATE sectors_meta sm
SET is_cc = true
WHERE sm.is_cc = false
  AND sm.orig_sealed_cid = sm.cur_sealed_cid
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

-- Backfill 2: Sectors incorrectly marked is_cc=true that were actually snapped
-- (orig_sealed_cid != cur_sealed_cid means snap completed on chain)
UPDATE sectors_meta sm
SET is_cc = false
WHERE sm.is_cc = true
  AND sm.orig_sealed_cid != sm.cur_sealed_cid
  AND NOT EXISTS (
      SELECT 1 FROM sectors_snap_pipeline snp
      WHERE snp.sp_id = sm.sp_id
        AND snp.sector_number = sm.sector_num
  );
