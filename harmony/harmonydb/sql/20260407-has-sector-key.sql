-- Add has_sector_key column to sectors_meta.
-- This mirrors chain state: true when the on-chain sector has SectorKeyCID set
-- (i.e. the sector has been snap-upgraded). It replaces the unreliable
-- orig_sealed_cid != cur_sealed_cid heuristic which breaks when a snap-deal
-- encodes all-zero data, producing an identical CommR.
--
-- Backfill of this column happens in the SectorMetadata reconciliation task
-- which reads chain state for every sector. No SQL-level backfill is performed
-- because the DB cannot call the chain API.

ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS has_sector_key BOOLEAN NOT NULL DEFAULT FALSE;

-- Seed the column from the best heuristic available in SQL: if the CIDs differ
-- the sector was definitely snapped. This covers the common case immediately;
-- zero-data-snap sectors (where CIDs match) will be fixed by the metadata task.
UPDATE sectors_meta SET has_sector_key = TRUE
WHERE orig_sealed_cid != cur_sealed_cid;

-- Replace the BEFORE INSERT/UPDATE trigger on sectors_meta to use has_sector_key
-- instead of orig_sealed_cid = cur_sealed_cid.
CREATE OR REPLACE FUNCTION update_is_cc()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.is_cc := (NOT NEW.has_sector_key) AND NOT EXISTS (
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
    SET is_cc = (NOT sectors_meta.has_sector_key) AND NOT EXISTS (
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
