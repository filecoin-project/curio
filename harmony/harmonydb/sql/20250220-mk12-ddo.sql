ALTER TABLE market_mk12_deal_pipeline
    ADD COLUMN is_ddo BOOLEAN DEFAULT FALSE NOT NULL;

ALTER TABLE market_direct_deals
    ADD COLUMN error TEXT DEFAULT NULL;

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
CREATE TRIGGER check_duplicate_successful_mk12ddo_deals
    BEFORE INSERT ON market_direct_deals
    FOR EACH ROW
    EXECUTE FUNCTION prevent_duplicate_successful_mk12ddo_deals()
