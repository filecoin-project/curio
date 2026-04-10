CREATE OR REPLACE FUNCTION insert_ad_and_update_head_checked(
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
    _entries TEXT,
    _expected_previous TEXT
) RETURNS BOOLEAN AS $$
DECLARE
    _current_head TEXT;
    _rows BIGINT;
BEGIN
    SELECT head INTO _current_head
    FROM ipni_head
    WHERE provider = _provider;

    IF _current_head IS DISTINCT FROM _expected_previous THEN
        RETURN FALSE;
    END IF;

    INSERT INTO ipni (
        ad_cid, context_id, metadata, is_rm, previous, provider, addresses,
        signature, entries, piece_cid_v2, piece_cid, piece_size
    )
    VALUES (
        _ad_cid, _context_id, _metadata, _is_rm, _expected_previous, _provider, _addresses,
        _signature, _entries, _piece_cid_v2, _piece_cid, _piece_size
    );

    IF _expected_previous IS NULL THEN
        INSERT INTO ipni_head (provider, head)
        VALUES (_provider, _ad_cid)
        ON CONFLICT (provider) DO NOTHING;
    ELSE
        UPDATE ipni_head
        SET head = _ad_cid
        WHERE provider = _provider
          AND head IS NOT DISTINCT FROM _expected_previous;
    END IF;

    GET DIAGNOSTICS _rows = ROW_COUNT;
    RETURN _rows = 1;
END;
$$ LANGUAGE plpgsql;
