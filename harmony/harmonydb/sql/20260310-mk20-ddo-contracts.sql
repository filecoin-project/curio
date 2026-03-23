CREATE TABLE IF NOT EXISTS ddo_contracts (
    address TEXT PRIMARY KEY,
    allowed BOOLEAN NOT NULL DEFAULT FALSE
);

-- Delete the older process_offline_download
DROP FUNCTION IF EXISTS process_offline_download(TEXT, TEXT, TEXT, BIGINT, TEXT);

-- This function triggers a download for an offline piece.
-- It is different from MK1.2 PoRep pipeline as it downloads the offline pieces
-- locally. This is to allow serving retrievals with piece park.
CREATE OR REPLACE FUNCTION process_offline_download(
  _id TEXT,
  _piece_cid_v2 TEXT,
  _piece_cid TEXT,
  _piece_size BIGINT,
  _raw_size BIGINT,
  _product TEXT
) RETURNS BOOLEAN AS $$
DECLARE
  _url TEXT;
  _headers JSONB;
  _deal_aggregation INT;
  _piece_id BIGINT;
  _ref_id BIGINT;
BEGIN
    -- 1. Early exit if no offline match found
    SELECT url, headers
    INTO _url, _headers
    FROM market_mk20_offline_urls
    WHERE id = _id AND piece_cid_v2 = _piece_cid_v2;

    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    -- 2. Get deal_aggregation flag
    SELECT deal_aggregation
    INTO _deal_aggregation
    FROM market_mk20_pipeline
    WHERE id = _id AND piece_cid_v2 = _piece_cid_v2 LIMIT 1;

    -- 3. Look for an existing piece
    SELECT id
    INTO _piece_id
    FROM parked_pieces
    WHERE piece_cid = _piece_cid AND piece_padded_size = _piece_size;

    -- 4. Insert piece if it is not found
    IF NOT FOUND THEN
        INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
        VALUES (_piece_cid, _piece_size, _raw_size, NOT (_deal_aggregation > 0))
        RETURNING id INTO _piece_id;
    END IF;

    -- 5. Insert piece ref
    INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
    VALUES (_piece_id, _url, _headers, NOT (_deal_aggregation > 0))
        RETURNING ref_id INTO _ref_id;

    -- 6. Insert or update download pipeline with ref_id
    INSERT INTO market_mk20_download_pipeline (id, piece_cid_v2, product, ref_ids)
    VALUES (_id, _piece_cid_v2, _product, ARRAY[_ref_id])
    ON CONFLICT (id, piece_cid_v2, product) DO UPDATE
    SET ref_ids = (
        SELECT ARRAY(
            SELECT DISTINCT r
            FROM unnest(market_mk20_download_pipeline.ref_ids || excluded.ref_ids) AS r
        )
    );

    -- 7. Mark the deal as started
    UPDATE market_mk20_pipeline
    SET started = TRUE
    WHERE id = _id AND piece_cid_v2 = _piece_cid_v2 AND started = FALSE;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;
