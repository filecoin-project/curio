-- Maintain piece_summary from row deltas instead of recounting both source tables
-- for every metadata or advertisement change.

CREATE OR REPLACE FUNCTION update_piece_summary()
RETURNS TRIGGER AS $$
DECLARE
    total_delta BIGINT := 0;
    indexed_delta BIGINT := 0;
    announced_delta BIGINT := 0;
BEGIN
    IF TG_OP = 'INSERT' THEN
        total_delta := 1;
        indexed_delta := CASE WHEN NEW.indexed THEN 1 ELSE 0 END;

        SELECT COUNT(*)
        INTO announced_delta
        FROM ipni
        WHERE piece_cid = NEW.piece_cid
          AND piece_size = NEW.piece_size;
    ELSIF TG_OP = 'DELETE' THEN
        total_delta := -1;
        indexed_delta := CASE WHEN OLD.indexed THEN -1 ELSE 0 END;

        SELECT -COUNT(*)
        INTO announced_delta
        FROM ipni
        WHERE piece_cid = OLD.piece_cid
          AND piece_size = OLD.piece_size;
    ELSIF TG_OP = 'UPDATE' THEN
        indexed_delta :=
            (CASE WHEN NEW.indexed THEN 1 ELSE 0 END) -
            (CASE WHEN OLD.indexed THEN 1 ELSE 0 END);

        IF (NEW.piece_cid, NEW.piece_size) IS DISTINCT FROM
           (OLD.piece_cid, OLD.piece_size) THEN
            SELECT
                (SELECT COUNT(*)
                 FROM ipni
                 WHERE piece_cid = NEW.piece_cid
                   AND piece_size = NEW.piece_size) -
                (SELECT COUNT(*)
                 FROM ipni
                 WHERE piece_cid = OLD.piece_cid
                   AND piece_size = OLD.piece_size)
            INTO announced_delta;
        END IF;
    END IF;

    IF total_delta = 0 AND indexed_delta = 0 AND announced_delta = 0 THEN
        RETURN NULL;
    END IF;

    UPDATE piece_summary
    SET total = total + total_delta,
        indexed = indexed + indexed_delta,
        announced = announced + announced_delta,
        last_updated = NOW()
    WHERE id = TRUE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'piece_summary row is missing';
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_piece_summary_ipni()
RETURNS TRIGGER AS $$
DECLARE
    announced_delta BIGINT := 0;
BEGIN
    -- Preserve the existing meaning: announced counts matching IPNI history rows,
    -- including removal advertisements.
    IF TG_OP = 'INSERT' THEN
        IF EXISTS (
            SELECT 1
            FROM market_piece_metadata
            WHERE piece_cid = NEW.piece_cid
              AND piece_size = NEW.piece_size
        ) THEN
            announced_delta := 1;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        IF EXISTS (
            SELECT 1
            FROM market_piece_metadata
            WHERE piece_cid = OLD.piece_cid
              AND piece_size = OLD.piece_size
        ) THEN
            announced_delta := -1;
        END IF;
    ELSIF TG_OP = 'UPDATE' AND
          (NEW.piece_cid, NEW.piece_size) IS DISTINCT FROM
          (OLD.piece_cid, OLD.piece_size) THEN
        announced_delta :=
            CASE WHEN EXISTS (
                SELECT 1
                FROM market_piece_metadata
                WHERE piece_cid = NEW.piece_cid
                  AND piece_size = NEW.piece_size
            ) THEN 1 ELSE 0 END -
            CASE WHEN EXISTS (
                SELECT 1
                FROM market_piece_metadata
                WHERE piece_cid = OLD.piece_cid
                  AND piece_size = OLD.piece_size
            ) THEN 1 ELSE 0 END;
    END IF;

    -- Most PDP advertisements have no market_piece_metadata row. Avoid touching
    -- the singleton summary row when the advertisement cannot affect the count.
    IF announced_delta = 0 THEN
        RETURN NULL;
    END IF;

    UPDATE piece_summary
    SET announced = announced + announced_delta,
        last_updated = NOW()
    WHERE id = TRUE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'piece_summary row is missing';
    END IF;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_update_piece_summary ON market_piece_metadata;
CREATE TRIGGER trigger_update_piece_summary
    AFTER INSERT OR DELETE OR UPDATE OF piece_cid, piece_size, indexed ON market_piece_metadata
    FOR EACH ROW
    EXECUTE FUNCTION update_piece_summary();

DROP TRIGGER IF EXISTS trigger_update_piece_summary_ipni ON ipni;
CREATE TRIGGER trigger_update_piece_summary_ipni
    AFTER INSERT OR DELETE OR UPDATE OF piece_cid, piece_size ON ipni
    FOR EACH ROW
    EXECUTE FUNCTION update_piece_summary_ipni();

-- Existing counters may be stale because the previous triggers did not handle
-- metadata deletion. Establish one authoritative baseline before using deltas.
WITH metadata_counts AS (
    SELECT
        COUNT(*) AS total,
        COUNT(*) FILTER (WHERE indexed = TRUE) AS indexed
    FROM market_piece_metadata
), announced_count AS (
    SELECT COUNT(*) AS announced
    FROM market_piece_metadata mpm
    JOIN ipni i
      ON mpm.piece_cid = i.piece_cid
     AND mpm.piece_size = i.piece_size
)
UPDATE piece_summary
SET total = metadata_counts.total,
    indexed = metadata_counts.indexed,
    announced = announced_count.announced,
    last_updated = NOW()
FROM metadata_counts, announced_count
WHERE id = TRUE;
