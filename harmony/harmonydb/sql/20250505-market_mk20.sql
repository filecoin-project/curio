CREATE TABLE ddo_contracts (
    address TEXT NOT NULL PRIMARY KEY,
    abi TEXT NOT NULL
);

CREATE TABLE market_mk20_deal (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT PRIMARY KEY,
    piece_cid TEXT NOT NULL,
    size BIGINT NOT NULL,

    format JSONB NOT NULL,
    source_http JSONB NOT NULL DEFAULT 'null',
    source_aggregate JSONB NOT NULL DEFAULT 'null',
    source_offline JSONB NOT NULL DEFAULT 'null',

    ddov1 JSONB NOT NULL DEFAULT 'null',
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
    start_time TIMESTAMPZ DEFAULT NULL
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
    PRIMARY KEY (id, piece_cid, piece_size),
    CONSTRAINT market_mk20_offline_urls_id_fk FOREIGN KEY (id)
      REFERENCES market_mk20_pipeline (id)
      ON DELETE CASCADE,
    CONSTRAINT market_mk20_offline_urls_id_unique UNIQUE (id)
);

CREATE TABLE market_mk20_products (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

CREATE TABLE market_mk20_data_source (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

INSERT INTO market_mk20_products (name, enabled) VALUES ('ddov1', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('http', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('aggregate', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('offline', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('put', TRUE);





