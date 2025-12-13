-- Piece summary table. This table will always have 1 row only and will be updated
-- by triggers
CREATE TABLE IF NOT EXISTS piece_summary (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE, -- Single-row identifier, always set to TRUE
    total BIGINT NOT NULL DEFAULT 0,
    indexed BIGINT NOT NULL DEFAULT 0,
    announced BIGINT NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW())
);

-- Insert the initial row
INSERT INTO piece_summary (id) VALUES (TRUE) ON CONFLICT DO NOTHING;

-- Function to update piece_summary when a new entry is added to market_piece_metadata
CREATE OR REPLACE FUNCTION update_piece_summary()
RETURNS TRIGGER AS $$
DECLARE
    total_count BIGINT;
    indexed_count BIGINT;
    announced_count BIGINT;
BEGIN
    -- Count total entries in market_piece_metadata
    SELECT COUNT(*) INTO total_count FROM market_piece_metadata;

    -- Count entries in market_piece_metadata where indexed is true
    SELECT COUNT(*) INTO indexed_count FROM market_piece_metadata WHERE indexed = TRUE;

    -- Count entries in market_piece_metadata that match entries in ipni on piece_cid and piece_size
    SELECT COUNT(*) INTO announced_count
    FROM market_piece_metadata mpm
             JOIN ipni i ON mpm.piece_cid = i.piece_cid AND mpm.piece_size = i.piece_size;

    -- Update piece_summary with the new counts and set last_updated to now
    UPDATE piece_summary
    SET
        total = total_count,
        indexed = indexed_count,
        announced = announced_count,
        last_updated = TIMEZONE('UTC', NOW());

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to call update_piece_summary function on insert to market_piece_metadata
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'trigger_update_piece_summary'
    ) THEN
        CREATE TRIGGER trigger_update_piece_summary AFTER INSERT OR UPDATE ON market_piece_metadata
    FOR EACH ROW
    EXECUTE FUNCTION update_piece_summary();
    END IF;
END $$;





