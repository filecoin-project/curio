DROP FUNCTION IF EXISTS insert_ad_and_update_head_checked(
    TEXT, BYTEA, BYTEA, TEXT, TEXT, BIGINT, BOOLEAN, TEXT, TEXT, BYTEA, TEXT, TEXT
);

CREATE OR REPLACE FUNCTION insert_ad_and_update_head(
    _ad_cid TEXT,
    _context_id BYTEA,
    _metadata BYTEA,
    _piece_cid_v2 TEXT,
    _piece_cid TEXT,
    _piece_size BIGINT,
    _is_rm BOOLEAN,
    _provider TEXT,
    _addresses TEXT,
    _signature BYTEA,
    _entries TEXT
) RETURNS VOID AS $$
DECLARE
    _previous TEXT;
    _new_order BIGINT;
BEGIN
    SELECT head INTO _previous
    FROM ipni_head
    WHERE provider = _provider;

    INSERT INTO ipni (ad_cid, context_id, metadata, is_rm, previous, provider, addresses, signature, entries, piece_cid_v2, piece_cid, piece_size)
    VALUES (_ad_cid, _context_id, _metadata, _is_rm, _previous, _provider, _addresses, _signature, _entries, _piece_cid_v2, _piece_cid, _piece_size);

    INSERT INTO ipni_head (provider, head)
    VALUES (_provider, _ad_cid)
        ON CONFLICT (provider) DO UPDATE SET head = EXCLUDED.head;

END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION mk20_pdp_mark_downloaded(_product text)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    updated_count int := 0;
BEGIN
    WITH candidates AS (
        SELECT p.id, p.piece_cid_v2, dp.ref_ids
        FROM pdp_pipeline p
        JOIN market_mk20_download_pipeline dp
          ON dp.id = p.id
          AND dp.piece_cid_v2 = p.piece_cid_v2
          AND dp.product = _product
        WHERE p.piece_ref IS NULL
    ),
    picked AS (
        SELECT c.id, c.piece_cid_v2, c.ref_ids, ch.ref_id AS chosen_ref
        FROM candidates c
        CROSS JOIN LATERAL (
            SELECT pr.ref_id
            FROM unnest(c.ref_ids) AS r(ref_id)
            JOIN parked_piece_refs pr ON pr.ref_id = r.ref_id
            JOIN parked_pieces pp ON pp.id = pr.piece_id
            WHERE pp.complete = TRUE
            LIMIT 1
        ) ch
    ),
    del_other_refs AS (
        DELETE FROM parked_piece_refs pr
        USING picked
        WHERE pr.ref_id = ANY(picked.ref_ids)
          AND pr.ref_id != picked.chosen_ref
        RETURNING 1
    ),
    del_download_rows AS (
        DELETE FROM market_mk20_download_pipeline dp
        USING picked
        WHERE dp.id = picked.id
          AND dp.piece_cid_v2 = picked.piece_cid_v2
          AND dp.product = _product
        RETURNING 1
    ),
    upd AS (
        UPDATE pdp_pipeline p
        SET downloaded = TRUE,
            piece_ref  = picked.chosen_ref
        FROM picked
        WHERE p.id = picked.id
          AND p.piece_cid_v2 = picked.piece_cid_v2
        RETURNING 1
    )
    SELECT count(*) INTO updated_count FROM upd;

    RETURN updated_count;
END;
$$;

CREATE OR REPLACE FUNCTION mk20_ddo_mark_downloaded(_product text)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    updated_count int := 0;
BEGIN
    WITH candidates AS (
        SELECT p.id, p.piece_cid_v2, dp.ref_ids
        FROM market_mk20_pipeline p
        JOIN market_mk20_download_pipeline dp
          ON dp.id = p.id
             AND dp.piece_cid_v2 = p.piece_cid_v2
             AND dp.product = _product
        WHERE p.url IS NULL
    ),
    picked AS (
        SELECT c.id, c.piece_cid_v2, c.ref_ids, ch.ref_id AS chosen_ref
        FROM candidates c
        CROSS JOIN LATERAL (
            SELECT pr.ref_id
            FROM unnest(c.ref_ids) AS r(ref_id)
            JOIN parked_piece_refs pr ON pr.ref_id = r.ref_id
            JOIN parked_pieces pp ON pp.id = pr.piece_id
            WHERE pp.complete = TRUE
            LIMIT 1
        ) ch
    ),
    del_other_refs AS (
        DELETE FROM parked_piece_refs pr
        USING picked
        WHERE pr.ref_id = ANY(picked.ref_ids)
          AND pr.ref_id != picked.chosen_ref
        RETURNING 1
    ),
    del_download_rows AS (
        DELETE FROM market_mk20_download_pipeline dp
        USING picked
        WHERE dp.id = picked.id
          AND dp.piece_cid_v2 = picked.piece_cid_v2
          AND dp.product = _product
        RETURNING 1
    ),
    upd AS (
        UPDATE market_mk20_pipeline p
        SET downloaded = TRUE,
            url        = 'pieceref:' || picked.chosen_ref::text
        FROM picked
        WHERE p.id = picked.id
          AND p.piece_cid_v2 = picked.piece_cid_v2
        RETURNING 1
    )
    SELECT count(*) INTO updated_count FROM upd;

    RETURN updated_count;
END;
$$;

DROP INDEX IF EXISTS parked_pieces_active_piece_key;

CREATE OR REPLACE FUNCTION process_offline_download(
  _id TEXT,
  _piece_cid_v2 TEXT,
  _piece_cid TEXT,
  _piece_size BIGINT,
  _product TEXT
) RETURNS BOOLEAN AS $$
DECLARE
  _url TEXT;
  _headers JSONB;
  _raw_size BIGINT;
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
