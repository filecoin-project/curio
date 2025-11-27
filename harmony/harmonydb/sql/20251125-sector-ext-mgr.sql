CREATE TABLE IF NOT EXISTS sectors_meta_updates (
    singleton BOOLEAN NOT NULL DEFAULT FALSE CHECK (singleton = FALSE),

    last_refresh_at TIMESTAMP WITH TIME ZONE,
    last_refresh_epoch BIGINT,
    last_refresh_tsk BYTEA
);

CREATE TABLE IF NOT EXISTS sectors_exp_buckets (
    less_than_days INT NOT NULL PRIMARY KEY
);

CREATE INDEX IF NOT EXISTS sectors_exp_buckets_sorted_idx ON sectors_exp_buckets (less_than_days ASC);

-- 1, 2, 3 weeks, useful for rolling deal extensions and main CC sector pool
-- 180 days, 210 days, useful for rolling cc sector pools
-- 360 days, 390 days, useful for rolling cc sector pools
-- 540 days, 570 days, useful for rolling cc sector pools
INSERT INTO sectors_exp_buckets (less_than_days) VALUES (7), (14), (21), (28), (180), (210), (360), (390), (540), (570) ON CONFLICT DO NOTHING;

-- Expiration manager
-- Action types:
-- 'extend' - "if any sector is in bucket A < exp < B, then extend all sectors A < exp < C to expiration D"
--   - e.g. if any sector is expiring between 0 and 2 weeks extend all sectors between 0 and 3 weeks to 4 weeks
-- 'top_up' - "if count(A < exp < B) < C then top up the bucket to D sectors, taking sectors from any duration less than A"
--   - e.g. if there are less than 10 sectors expiring between 180 and 210 days, top up the bucket to 10 sectors, taking sectors from any duration less than 180 days
CREATE TABLE IF NOT EXISTS sectors_exp_manager_presets (
    name TEXT NOT NULL PRIMARY KEY,

    action_type TEXT NOT NULL CHECK (action_type IN ('extend', 'top_up')),
    
    -- info bucket we look at to determine if we need to extend or top up (both action types)
    info_bucket_above_days INT NOT NULL,
    info_bucket_below_days INT NOT NULL CHECK (info_bucket_above_days < info_bucket_below_days),
    
    -- target and max_extension expiration days in extend case, null for top_up (top_up extends to info_bucket_below_days)
    target_expiration_days BIGINT,
    max_candidate_days BIGINT, -- C in 'extend' action type

    -- top up count in top_up case, null for extend
    top_up_count_low_water_mark BIGINT,
    top_up_count_high_water_mark BIGINT,

    cc BOOLEAN, -- if true, only extend/top up CC sectors, if false just deals, if null - both
    drop_claims BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Ensure extend action has required fields
    CHECK (action_type != 'extend' OR (target_expiration_days IS NOT NULL AND max_candidate_days IS NOT NULL)),
    -- Ensure top_up action has required fields
    CHECK (action_type != 'top_up' OR (top_up_count_low_water_mark IS NOT NULL AND top_up_count_high_water_mark IS NOT NULL)),
    -- Ensure low water mark is less than high water mark for top_up
    CHECK (action_type != 'top_up' OR top_up_count_low_water_mark < top_up_count_high_water_mark)
);

CREATE TABLE IF NOT EXISTS sectors_exp_manager_sp (
    sp_id BIGINT NOT NULL,
    preset_name TEXT NOT NULL,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,

    last_run_at TIMESTAMP WITH TIME ZONE,

    last_message_cid TEXT,
    last_message_landed_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (sp_id, preset_name),
    FOREIGN KEY (preset_name) REFERENCES sectors_exp_manager_presets(name) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS sectors_exp_manager_sp_last_message_cid_idx ON sectors_exp_manager_sp (last_message_cid);

-- insert default presets
INSERT INTO sectors_exp_manager_presets (name, action_type, info_bucket_above_days, info_bucket_below_days, target_expiration_days, max_candidate_days, top_up_count_low_water_mark, top_up_count_high_water_mark, cc, drop_claims) VALUES
('roll_all_near_expiration', 'extend', 0, 14, 28, 21, NULL, NULL, NULL, FALSE), -- any in 0..14 days: extend all 0..21 days -> 28 days
('cc_180d_pool',             'top_up', 180, 210, NULL, NULL, 100, 200, TRUE, FALSE), -- if less than 100 CC in 180..210 days: top up to 200 CC in 180..210 days
('cc_360d_pool',             'top_up', 360, 390, NULL, NULL, 100, 200, TRUE, FALSE), -- if less than 100 CC in 360..390 days: top up to 200 CC in 360..390 days
('cc_540d_pool',             'top_up', 540, 570, NULL, NULL, 100, 200, TRUE, FALSE) -- if less than 100 CC in 540..570 days: top up to 200 CC in 540..570 days
ON CONFLICT DO NOTHING;

ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS min_claim_epoch BIGINT;
ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS max_claim_epoch BIGINT;

-- Function to evaluate if a preset condition is met for an SP
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

CREATE OR REPLACE FUNCTION update_sectors_exp_manager_sp_from_message_waits()
RETURNS trigger AS $$
BEGIN
  IF OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL THEN
    UPDATE sectors_exp_manager_sp
      SET last_message_landed_at = current_timestamp
    WHERE last_message_cid = NEW.signed_message_cid;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'tr_update_sectors_exp_manager_sp_from_message_waits'
    ) THEN
        CREATE TRIGGER tr_update_sectors_exp_manager_sp_from_message_waits AFTER UPDATE ON message_waits
FOR EACH ROW
WHEN (OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL)
EXECUTE FUNCTION update_sectors_exp_manager_sp_from_message_waits();
    END IF;
END $$;

