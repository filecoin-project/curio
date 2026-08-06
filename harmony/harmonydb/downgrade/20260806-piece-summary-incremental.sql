-- Restore the full-recount piece summary trigger behavior.

DROP TRIGGER IF EXISTS trigger_update_piece_summary ON market_piece_metadata;
DROP TRIGGER IF EXISTS trigger_update_piece_summary_ipni ON ipni;

CREATE OR REPLACE FUNCTION update_piece_summary()
RETURNS TRIGGER AS $$
DECLARE
    total_count BIGINT;
    indexed_count BIGINT;
    announced_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO total_count FROM market_piece_metadata;

    SELECT COUNT(*) INTO indexed_count
    FROM market_piece_metadata
    WHERE indexed = TRUE;

    SELECT COUNT(*) INTO announced_count
    FROM market_piece_metadata mpm
    JOIN ipni i
      ON mpm.piece_cid = i.piece_cid
     AND mpm.piece_size = i.piece_size;

    UPDATE piece_summary
    SET total = total_count,
        indexed = indexed_count,
        announced = announced_count,
        last_updated = TIMEZONE('UTC', NOW());

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS update_piece_summary_ipni();

CREATE TRIGGER trigger_update_piece_summary
    AFTER INSERT OR UPDATE ON market_piece_metadata
    FOR EACH ROW
    EXECUTE FUNCTION update_piece_summary();

CREATE TRIGGER trigger_update_piece_summary_ipni
    AFTER INSERT OR UPDATE ON ipni
    FOR EACH ROW
    EXECUTE FUNCTION update_piece_summary();
