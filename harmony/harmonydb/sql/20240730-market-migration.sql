-- Table for Mk12 or Boost deals
CREATE TABLE market_mk12_deals (
    uuid TEXT NOT NULL,
    sp_id BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    signed_proposal_cid TEXT NOT NULL,
    proposal_signature BYTEA NOT NULL,
    proposal jsonb NOT NULL,

    offline BOOLEAN NOT NULL,
    verified BOOLEAN NOT NULL,

    start_epoch BIGINT NOT NULL,
    end_epoch BIGINT NOT NULL,

    client_peer_id TEXT NOT NULL,

    chain_deal_id BIGINT DEFAULT NULL,
    publish_cid TEXT DEFAULT NULL,

    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    length BIGINT DEFAULT NULL,

    fast_retrieval BOOLEAN NOT NULL,
    announce_to_ipni BOOLEAN NOT NULL,

    url TEXT DEFAULT NULL,
    url_headers jsonb NOT NULL DEFAULT '{}',

    error TEXT DEFAULT NULL,

    primary key (uuid, sp_id, piece_cid, signed_proposal_cid),
    unique (uuid),
    unique (signed_proposal_cid)
);

-- Table for old lotus market deals. This is just for deal
-- which are still alive. It should not be used for any processing
CREATE TABLE market_legacy_deals (
    signed_proposal_cid TEXT,
    proposal_signature BYTEA,
    proposal jsonb,
    piece_cid TEXT,
    piece_size BIGINT,
    offline BOOLEAN,
    verified BOOLEAN,
    sp_id BIGINT,
    start_epoch BIGINT,
    end_epoch BIGINT,
    publish_cid TEXT,
    fast_retrieval BOOLEAN,
    chain_deal_id BIGINT,
    created_at TIMESTAMPTZ,
    sector_num BIGINT,

    primary key (sp_id, piece_cid, signed_proposal_cid)
);

-- This table is used for storing piece metadata (piece indexing)
CREATE TABLE market_piece_metadata (
    piece_cid TEXT NOT NULL PRIMARY KEY,
    version INT NOT NULL DEFAULT 2,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    indexed BOOLEAN NOT NULL DEFAULT FALSE,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    constraint market_piece_meta_identity_key
        unique (piece_cid)
);

-- This table binds the piece metadata to specific deals (piece indexing)
CREATE TABLE market_piece_deal (
    id TEXT NOT NULL, -- (UUID for new deals, PropCID for old)
    piece_cid TEXT NOT NULL,

    boost_deal BOOLEAN NOT NULL,
    legacy_deal BOOLEAN NOT NULL DEFAULT FALSE,

    chain_deal_id BIGINT NOT NULL DEFAULT 0,

    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,

    piece_offset BIGINT NOT NULL,
    piece_length BIGINT NOT NULL,
    raw_size BIGINT NOT NULL,

    primary key (sp_id, piece_cid, id),
    constraint market_piece_deal_identity_key
        unique (sp_id, id)
);

-- This function is used to insert piece metadata and piece deal (piece indexing)
CREATE OR REPLACE FUNCTION process_piece_deal(
    _id TEXT,
    _piece_cid TEXT,
    _boost_deal BOOLEAN,
    _sp_id BIGINT,
    _sector_num BIGINT,
    _piece_offset BIGINT,
    _piece_length BIGINT,
    _raw_size BIGINT,
    _legacy_deal BOOLEAN DEFAULT FALSE,
    _chain_deal_id BIGINT DEFAULT 0
)
RETURNS VOID AS $$
BEGIN
INSERT INTO market_piece_metadata (piece_cid, indexed) VALUES (_piece_cid, TRUE)
    ON CONFLICT (piece_cid) DO UPDATE SET indexed = TRUE;

INSERT INTO market_piece_deal (
    id, piece_cid, boost_deal, legacy_deal, chain_deal_id,
    sp_id, sector_num, piece_offset, piece_length, raw_size
) VALUES (
           _id, _piece_cid, _boost_deal, _legacy_deal, _chain_deal_id,
           _sp_id, _sector_num, _piece_offset, _piece_length, _raw_size
       ) ON CONFLICT (sp_id, piece_cid, id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

-- Storage Ask for ask protocol
CREATE TABLE market_mk12_storage_ask (
    sp_id BIGINT NOT NULL,

    price BIGINT NOT NULL,
    verified_price BIGINT NOT NULL,

    min_size BIGINT NOT NULL,
    max_size BIGINT NOT NULL,

    created_at BIGINT NOT NULL,
    expiry BIGINT NOT NULL,

    sequence BIGINT NOT NULL,
    unique (sp_id)
);

-- Used for processing Mk12 deals
CREATE TABLE market_mk12_deal_pipeline (
    uuid TEXT NOT NULL,
    sp_id BIGINT NOT NULL,

    started BOOLEAN DEFAULT FALSE,

    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,
    file_size BIGINT DEFAULT NULL, -- raw piece size

    offline BOOLEAN NOT NULL,

    url TEXT DEFAULT NULL,
    headers jsonb NOT NULL DEFAULT '{}',

    commp_task_id BIGINT DEFAULT NULL,
    after_commp BOOLEAN DEFAULT FALSE,

    psd_task_id BIGINT DEFAULT NULL,
    after_psd BOOLEAN DEFAULT FALSE,

    psd_wait_time TIMESTAMPTZ,

    find_deal_task_id BIGINT DEFAULT NULL,
    after_find_deal BOOLEAN DEFAULT FALSE,

    sector BIGINT,
    sector_offset BIGINT,

    sealed BOOLEAN DEFAULT FALSE,
    indexed BOOLEAN DEFAULT FALSE,

    constraint market_mk12_deal_pipeline_identity_key unique (uuid)
);

-- This table can be used to track remote piece for offline deals
-- The entries must be created by users
CREATE TABLE market_offline_urls (
    piece_cid TEXT NOT NULL,

    url TEXT NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',

    raw_size BIGINT NOT NULL,

    unique (piece_cid)
);

-- indexing tracker is separate from
CREATE TABLE market_indexing_tasks (
    uuid TEXT NOT NULL,

    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    reg_seal_proof INT NOT NULL,

    piece_offset BIGINT NOT NULL,
    piece_size BIGINT NOT NULL,
    raw_size BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    task_id BIGINT DEFAULT NULL,

    constraint market_indexing_tasks_identity_key
       unique (id, sp_id, sector_number, piece_offset, piece_size, piece_cid, reg_seal_proof)
);

CREATE TABLE libp2p_keys (
    sp_id BIGINT NOT NULL,
    priv_key BYTEA NOT NULL,
    listen_address TEXT NOT NULL,
    announce_address TEXT NOT NULL,
    no_announce_address TEXT NOT NULL
);

ALTER TABLE parked_piece_refs
    ADD COLUMN host text;

create table file_parked_pieces (
    id bigserial primary key,
    created_at timestamp default current_timestamp,
    piece_cid text not null,
    piece_padded_size bigint not null,
    piece_raw_size bigint not null,
    complete boolean not null default false,
    task_id bigint default null,
    cleanup_task_id bigint default null,
    unique (piece_cid)
);

/*
 * This table is used to keep track of the references to the file parked pieces
 * so that we can delete them when they are no longer needed.
 *
 * All references into the file_parked_pieces table should be done through this table.
 */
create table file_parked_piece_refs (
    ref_id bigserial primary key,
    piece_id bigint not null,
    data_url text not null,
    node text not null,
    foreign key (piece_id) references file_parked_pieces(id) on delete cascade
);




