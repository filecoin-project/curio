-- Drop the existing primary key constraint for market_piece_metadata
ALTER TABLE market_piece_metadata
DROP CONSTRAINT market_piece_metadata_pkey;

-- Drop the redundant UNIQUE constraint if it exists for market_piece_metadata
ALTER TABLE market_piece_metadata
DROP CONSTRAINT IF EXISTS market_piece_meta_identity_key;

-- Add the new composite primary key for market_piece_metadata
ALTER TABLE market_piece_metadata
    ADD PRIMARY KEY (piece_cid, piece_size);

-- Drop the current primary key for market_piece_deal
ALTER TABLE market_piece_deal
DROP CONSTRAINT market_piece_deal_pkey;

-- Drop the old UNIQUE constraint for market_piece_deal
ALTER TABLE market_piece_deal
DROP CONSTRAINT IF EXISTS market_piece_deal_identity_key;

-- Add the new composite primary key for market_piece_deal
ALTER TABLE market_piece_deal
    ADD PRIMARY KEY (sp_id, id, piece_cid, piece_length);

-- Add a column to relate a piece park piece to mk20 deal
ALTER TABLE market_piece_deal
ADD COLUMN piece_ref BIGINT;

-- This function is used to insert piece metadata and piece deal (piece indexing)
-- This makes it easy to keep the logic of how table is updated and fast (in DB).
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
     ) ON CONFLICT (sp_id, id, piece_cid, piece_length) DO NOTHING;

END;
$$ LANGUAGE plpgsql;

-- Add ID column to ipni_task table
ALTER TABLE ipni_task
    ADD COLUMN id TEXT;

-- Function to create ipni tasks
CREATE OR REPLACE FUNCTION insert_ipni_task(
    _id TEXT,
    _sp_id BIGINT,
    _sector BIGINT,
    _reg_seal_proof INT,
    _sector_offset BIGINT,
    _context_id BYTEA,
    _is_rm BOOLEAN,
    _provider TEXT,
    _task_id BIGINT DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
_existing_is_rm BOOLEAN;
    _latest_is_rm BOOLEAN;
BEGIN
    -- Check if ipni_task has the same context_id and provider with a different is_rm value
    SELECT is_rm INTO _existing_is_rm
    FROM ipni_task
    WHERE provider = _provider AND context_id = _context_id AND is_rm != _is_rm
            LIMIT 1;

    -- If a different is_rm exists for the same context_id and provider, insert the new task
    IF FOUND THEN
            INSERT INTO ipni_task (sp_id, id, sector, reg_seal_proof, sector_offset, provider, context_id, is_rm, created_at, task_id, complete)
            VALUES (_sp_id, _id, _sector, _reg_seal_proof, _sector_offset, _provider, _context_id, _is_rm, TIMEZONE('UTC', NOW()), _task_id, FALSE);
            RETURN;
    END IF;

    -- If no conflicting entry is found in ipni_task, check the latest ad in ipni table
    SELECT is_rm INTO _latest_is_rm
    FROM ipni
    WHERE provider = _provider AND context_id = _context_id
    ORDER BY order_number DESC
        LIMIT 1;

    -- If the latest ad has the same is_rm value, raise an exception
    IF FOUND AND _latest_is_rm = _is_rm THEN
            RAISE EXCEPTION 'already published';
    END IF;

    -- If all conditions are met, insert the new task into ipni_task
    INSERT INTO ipni_task (sp_id, id, sector, reg_seal_proof, sector_offset, provider, context_id, is_rm, created_at, task_id, complete)
    VALUES (_sp_id, _id, _sector, _reg_seal_proof, _sector_offset, _provider, _context_id, _is_rm, TIMEZONE('UTC', NOW()), _task_id, FALSE);
    END;
    $$ LANGUAGE plpgsql;


CREATE TABLE ddo_contracts (
    address TEXT NOT NULL PRIMARY KEY,
    abi TEXT NOT NULL
);

CREATE TABLE market_mk20_deal (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    sp_id BIGINT NOT NULL,

    id TEXT PRIMARY KEY,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,

    format JSONB NOT NULL,
    source_http JSONB NOT NULL DEFAULT 'null',
    source_aggregate JSONB NOT NULL DEFAULT 'null',
    source_offline JSONB NOT NULL DEFAULT 'null',
    source_http_put JSONB NOT NULL DEFAULT 'null',

    ddo_v1 JSONB NOT NULL DEFAULT 'null',
    market_deal_id TEXT DEFAULT NULL,

    error TEXT DEFAULT NULL
);

CREATE TABLE market_mk20_pipeline (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT NOT NULL,
    sp_id BIGINT NOT NULL,
    contract TEXT NOT NULL,
    client TEXT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    raw_size BIGINT NOT NULL,
    offline BOOLEAN NOT NULL,
    url TEXT DEFAULT NULL,
    indexing BOOLEAN NOT NULL,
    announce BOOLEAN NOT NULL,
    allocation_id BIGINT DEFAULT NULL,
    duration BIGINT NOT NULL,
    piece_aggregation INT NOT NULL DEFAULT 0,

    started BOOLEAN DEFAULT FALSE,

    downloaded BOOLEAN DEFAULT FALSE,

    commp_task_id BIGINT DEFAULT NULL,
    after_commp BOOLEAN DEFAULT FALSE,

    deal_aggregation INT NOT NULL DEFAULT 0,
    aggr_index BIGINT DEFAULT 0,
    agg_task_id BIGINT DEFAULT NULL,
    aggregated BOOLEAN DEFAULT FALSE,

    sector BIGINT DEFAULT NULL,
    reg_seal_proof INT DEFAULT NULL,
    sector_offset BIGINT DEFAULT NULL, -- padded offset

    sealed BOOLEAN DEFAULT FALSE,

    indexing_created_at TIMESTAMPTZ DEFAULT NULL,
    indexing_task_id BIGINT DEFAULT NULL,
    indexed BOOLEAN DEFAULT FALSE,

    complete BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (id, aggr_index)
);

CREATE TABLE market_mk20_pipeline_waiting (
    id TEXT PRIMARY KEY,
    waiting_for_data BOOLEAN DEFAULT FALSE,
    start_time TIMESTAMPTZ DEFAULT NULL
);

CREATE TABLE market_mk20_download_pipeline (
    id TEXT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    ref_ids BIGINT[] NOT NULL,
    PRIMARY KEY (id, piece_cid, piece_size)
);

CREATE TABLE market_mk20_offline_urls (
    id TEXT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    url TEXT NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    raw_size BIGINT NOT NULL,
    PRIMARY KEY (id, piece_cid, piece_size)
);

CREATE TABLE market_mk20_products (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

CREATE TABLE market_mk20_data_source (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

INSERT INTO market_mk20_products (name, enabled) VALUES ('ddo_v1', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('http', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('aggregate', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('offline', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('put', TRUE);

CREATE OR REPLACE FUNCTION process_offline_download(
  _id TEXT,
  _piece_cid TEXT,
  _piece_size BIGINT
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
    SELECT url, headers, raw_size
    INTO _url, _headers, _raw_size
    FROM market_mk20_offline_urls
    WHERE id = _id AND piece_cid = _piece_cid AND piece_size = _piece_size;

    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    -- 2. Get deal_aggregation flag
    SELECT deal_aggregation
    INTO _deal_aggregation
    FROM market_mk20_pipeline
    WHERE id = _id AND piece_cid = _piece_cid AND piece_size = _piece_size
      LIMIT 1;

    -- 3. Look for existing piece
    SELECT id
    INTO _piece_id
    FROM parked_pieces
    WHERE piece_cid = _piece_cid AND piece_padded_size = _piece_size;

    -- 4. Insert piece if not found
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
    INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids)
    VALUES (_id, _piece_cid, _piece_size, ARRAY[_ref_id])
    ON CONFLICT (id, piece_cid, piece_size) DO UPDATE
    SET ref_ids = (
        SELECT ARRAY(
            SELECT DISTINCT r
            FROM unnest(market_mk20_download_pipeline.ref_ids || excluded.ref_ids) AS r
        )
    );

    -- 7. Mark the deal as started
    UPDATE market_mk20_pipeline
    SET started = TRUE
    WHERE id = _id AND piece_cid = _piece_cid AND piece_size = _piece_size AND started = FALSE;

    RETURN TRUE;
END;
$$ LANGUAGE plpgsql;






