DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT
            p.oid::regprocedure AS sig
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE p.proname = 'process_piece_deal'
    LOOP
        EXECUTE format('DROP FUNCTION %s;', r.sig);
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION process_piece_deal(
    _id TEXT,
    _piece_cid TEXT,
    _boost_deal BOOLEAN,
    _sp_id BIGINT,
    _sector_num BIGINT,
    _piece_offset BIGINT,
    _piece_length BIGINT, -- padded length
    _raw_size BIGINT,
    _indexed BOOLEAN,
    _piece_ref BIGINT DEFAULT NULL,
    _legacy_deal BOOLEAN DEFAULT FALSE,
    _chain_deal_id BIGINT DEFAULT 0
)
    RETURNS VOID AS $$
BEGIN
    -- Insert or update the market_piece_metadata table
    INSERT INTO market_piece_metadata (piece_cid, piece_size, indexed)
    VALUES (_piece_cid, _piece_length, _indexed)
        ON CONFLICT (piece_cid, piece_size) DO UPDATE SET
        indexed = CASE
           WHEN market_piece_metadata.indexed = FALSE THEN EXCLUDED.indexed
           ELSE market_piece_metadata.indexed
         END;

    -- Insert into the market_piece_deal table
    INSERT INTO market_piece_deal (
        id, piece_cid, boost_deal, legacy_deal, chain_deal_id,
        sp_id, sector_num, piece_offset, piece_length, raw_size, piece_ref
    ) VALUES (
         _id, _piece_cid, _boost_deal, _legacy_deal, _chain_deal_id,
         _sp_id, _sector_num, _piece_offset, _piece_length, _raw_size, _piece_ref
     ) ON CONFLICT (id, sp_id, piece_cid, piece_length) DO NOTHING;

END;
$$ LANGUAGE plpgsql;