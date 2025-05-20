-- Drop the existing primary key constraint for market_piece_metadata
ALTER TABLE market_piece_metadata
DROP CONSTRAINT market_piece_metadata_pkey;

-- Drop the redundant UNIQUE constraint if it exists for market_piece_metadata
ALTER TABLE market_piece_metadata
DROP CONSTRAINT IF EXISTS market_piece_meta_identity_key;

-- Add the new composite primary key for market_piece_metadata
ALTER TABLE market_piece_metadata
    ADD PRIMARY KEY (piece_cid, piece_size);

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
    size BIGINT NOT NULL,

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
    piece_aggregation INT DEFAULT 0,

    started BOOLEAN DEFAULT FALSE,

    downloaded BOOLEAN DEFAULT FALSE,

    commp_task_id BIGINT DEFAULT NULL,
    after_commp BOOLEAN DEFAULT FALSE,

    deal_aggregation INT DEFAULT 0,
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
    started_put BOOLEAN DEFAULT FALSE,
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





