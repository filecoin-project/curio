ALTER TABLE market_mk12_deal_pipeline ADD COLUMN IF NOT EXISTS is_ddo BOOLEAN DEFAULT FALSE NOT NULL;

ALTER TABLE market_direct_deals ADD COLUMN IF NOT EXISTS error TEXT DEFAULT NULL;

-- prevent DDO deals with same allocation per SP_ID
CREATE OR REPLACE FUNCTION prevent_duplicate_successful_mk12ddo_deals()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if a deal already exists with the same sp_id & allocation_id and has no errors
    IF EXISTS (
        SELECT 1 FROM market_direct_deals
        WHERE sp_id = NEW.sp_id
        AND allocation_id = NEW.allocation_id
        AND (error IS NULL OR error = '')
    ) THEN
        RAISE EXCEPTION 'A successful deal already exists for sp_id = %, allocation_id = %', NEW.sp_id, NEW.allocation_id;
END IF;

RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger to prevent invalid inserts
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'check_duplicate_successful_mk12ddo_deals'
    ) THEN
        CREATE TRIGGER check_duplicate_successful_mk12ddo_deals BEFORE INSERT ON market_direct_deals
    FOR EACH ROW
    EXECUTE FUNCTION prevent_duplicate_successful_mk12ddo_deals();
    END IF;
END $$;

-- Attach trigger to regenerate announced count after an IPNI task
-- Otherwise, Announced count stays behind indexed count
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'trigger_update_piece_summary_ipni'
    ) THEN
        CREATE TRIGGER trigger_update_piece_summary_ipni AFTER INSERT OR UPDATE ON ipni
                        FOR EACH ROW
                        EXECUTE FUNCTION update_piece_summary();
    END IF;
END $$;
