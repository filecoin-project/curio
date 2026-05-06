-- Add is_live and is_faulty columns to sectors_meta for efficient filtering.
-- is_live: TRUE if sector is in the partition's LiveSectors bitfield on chain (not terminated)
-- is_faulty: TRUE if sector is in the partition's FaultySectors bitfield on chain
-- Defaults preserve existing behavior; SectorMetadata task syncs with chain state.
ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS is_live BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS is_faulty BOOLEAN NOT NULL DEFAULT FALSE;

-- Update eval_ext_mgr_sp_condition to filter out non-live and faulty sectors
CREATE OR REPLACE FUNCTION eval_ext_mgr_sp_condition(
    p_sp_id BIGINT,
    p_preset_name TEXT,
    p_curr_epoch BIGINT,
    p_epoch_per_day NUMERIC DEFAULT 2880
) RETURNS BOOLEAN AS $$
DECLARE
    v_preset RECORD;
    v_count BIGINT;
    v_bucket_above_epoch BIGINT;
    v_bucket_below_epoch BIGINT;
BEGIN
    -- Get preset configuration
    SELECT * INTO v_preset
    FROM sectors_exp_manager_presets
    WHERE name = p_preset_name;
    
    IF NOT FOUND THEN
        RETURN FALSE; -- Preset doesn't exist
    END IF;
    
    -- Calculate epoch boundaries for the info bucket
    v_bucket_above_epoch := p_curr_epoch + (v_preset.info_bucket_above_days * p_epoch_per_day);
    v_bucket_below_epoch := p_curr_epoch + (v_preset.info_bucket_below_days * p_epoch_per_day);
    
    IF v_preset.action_type = 'extend' THEN
        -- For 'extend': Check if ANY sector expires in the info bucket range
        -- Also filter by CC if specified
        -- Exclude sectors in snap pipeline or with open pieces
        SELECT COUNT(*) INTO v_count
        FROM sectors_meta sm
        WHERE sm.sp_id = p_sp_id
          AND sm.expiration_epoch IS NOT NULL
          AND sm.expiration_epoch > v_bucket_above_epoch
          AND sm.expiration_epoch < v_bucket_below_epoch
          AND sm.is_live = TRUE
          AND sm.is_faulty = FALSE
          AND (v_preset.cc IS NULL OR sm.is_cc = v_preset.cc)
          AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
          AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num);
        
        -- If any sector found in range, condition is met (needs extension)
        RETURN v_count > 0;
        
    ELSIF v_preset.action_type = 'top_up' THEN
        -- For 'top_up': Count sectors in the info bucket range
        -- Also filter by CC if specified
        -- Exclude sectors in snap pipeline or with open pieces
        SELECT COUNT(*) INTO v_count
        FROM sectors_meta sm
        WHERE sm.sp_id = p_sp_id
          AND sm.expiration_epoch IS NOT NULL
          AND sm.expiration_epoch > v_bucket_above_epoch
          AND sm.expiration_epoch < v_bucket_below_epoch
          AND sm.is_live = TRUE
          AND sm.is_faulty = FALSE
          AND (v_preset.cc IS NULL OR sm.is_cc = v_preset.cc)
          AND NOT EXISTS (SELECT 1 FROM sectors_snap_pipeline ssp WHERE ssp.sp_id = sm.sp_id AND ssp.sector_number = sm.sector_num)
          AND NOT EXISTS (SELECT 1 FROM open_sector_pieces osp WHERE osp.sp_id = sm.sp_id AND osp.sector_number = sm.sector_num);
        
        -- If count below low water mark, condition is met (needs top-up)
        RETURN v_count < COALESCE(v_preset.top_up_count_low_water_mark, 0);
        
    ELSE
        RETURN FALSE; -- Unknown action type
    END IF;
END;
$$ LANGUAGE plpgsql STABLE;
