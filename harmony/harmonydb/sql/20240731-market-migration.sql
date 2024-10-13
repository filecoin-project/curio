-- Table for Mk12 or Boost deals (Main deal table)
-- Stores the deal received over the network.
-- Entries are created by mk12 module and this will be used
-- by UI to show deal details. Entries should never be removed from this table.
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

    fast_retrieval BOOLEAN NOT NULL,
    announce_to_ipni BOOLEAN NOT NULL,

    url TEXT DEFAULT NULL,
    url_headers jsonb NOT NULL DEFAULT '{}',

    error TEXT DEFAULT NULL,

    primary key (uuid, sp_id, piece_cid, signed_proposal_cid),
    unique (uuid),
    unique (signed_proposal_cid)
);

-- This table is used for storing piece metadata (piece indexing). Entries are added by task_indexing.
-- It is also used to track if a piece is indexed or not.
-- Version is used to track changes of how metadata is stored.
-- Cleanup for this table will be created in a later stage.
CREATE TABLE market_piece_metadata (
    piece_cid TEXT NOT NULL PRIMARY KEY,

    version INT NOT NULL DEFAULT 2, -- Boost stored in version 1. This is version 2.

    created_at TIMESTAMPTZ  NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    indexed BOOLEAN NOT NULL DEFAULT FALSE,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    constraint market_piece_meta_identity_key
        unique (piece_cid)
);

-- This table binds the piece metadata to specific deals (piece indexing). Entries are added by task_indexing.
-- This along with market_mk12_deals is used to retrievals as well as
-- deal detail page in UI.
-- Cleanup for this table will be created in a later stage.
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
    _legacy_deal BOOLEAN DEFAULT FALSE,
    _chain_deal_id BIGINT DEFAULT 0
)
RETURNS VOID AS $$
BEGIN
    -- Insert or update the market_piece_metadata table
INSERT INTO market_piece_metadata (piece_cid, indexed)
VALUES (_piece_cid, _indexed)
    ON CONFLICT (piece_cid) DO UPDATE SET
    indexed = CASE
                WHEN market_piece_metadata.indexed = FALSE THEN EXCLUDED.indexed
                ELSE market_piece_metadata.indexed
END;

    -- Insert into the market_piece_deal table
INSERT INTO market_piece_deal (
    id, piece_cid, boost_deal, legacy_deal, chain_deal_id,
    sp_id, sector_num, piece_offset, piece_length, raw_size
    ) VALUES (
             _id, _piece_cid, _boost_deal, _legacy_deal, _chain_deal_id,
             _sp_id, _sector_num, _piece_offset, _piece_length, _raw_size
    ) ON CONFLICT (sp_id, piece_cid, id) DO NOTHING;

END;
$$ LANGUAGE plpgsql;

-- Storage Ask for ask protocol over libp2p
-- Entries for each MinerID must be present. These are updated by SetAsk method in mk12.
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

-- Used for processing Mk12 deals. This tables tracks the deal
-- throughout their lifetime. Entries are added ad the same time as market_mk12_deals.
-- Cleanup is done for complete deals by GC task.
CREATE TABLE market_mk12_deal_pipeline (
    uuid TEXT NOT NULL,
    sp_id BIGINT NOT NULL,

    started BOOLEAN DEFAULT FALSE,

    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL, -- padded size
    raw_size BIGINT DEFAULT NULL,

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

    sector BIGINT DEFAULT NULL,
    reg_seal_proof INT DEFAULT NULL,
    sector_offset BIGINT DEFAULT NULL, -- padded offset

    sealed BOOLEAN DEFAULT FALSE,

    should_index BOOLEAN DEFAULT FALSE,
    indexing_created_at TIMESTAMPTZ,
    indexing_task_id BIGINT DEFAULT NULL,
    indexed BOOLEAN DEFAULT FALSE,

    announce BOOLEAN DEFAULT FALSE,

    complete BOOLEAN NOT NULL DEFAULT FALSE,

    constraint market_mk12_deal_pipeline_identity_key unique (uuid)
);

-- This function creates indexing task based from move_storage tasks
CREATE OR REPLACE FUNCTION create_indexing_task(task_id BIGINT, sealing_table TEXT)
RETURNS VOID AS $$
DECLARE
query TEXT;   -- Holds the dynamic SQL query
    pms RECORD;   -- Holds each row returned by the query in the loop
BEGIN
    -- Construct the dynamic SQL query based on the sealing_table
    IF sealing_table = 'sectors_sdr_pipeline' THEN
        query := format(
            'SELECT
                dp.uuid,
                ssp.reg_seal_proof
            FROM
                %I ssp
            JOIN
                market_mk12_deal_pipeline dp ON ssp.sp_id = dp.sp_id AND ssp.sector_num = dp.sector
            WHERE
                ssp.task_id_move_storage = $1', sealing_table);
    ELSIF sealing_table = 'sectors_snap_pipeline' THEN
        query := format(
            'SELECT
                dp.uuid,
                (SELECT reg_seal_proof FROM sectors_meta WHERE sp_id = ssp.sp_id AND sector_num = ssp.sector_num) AS reg_seal_proof
            FROM
                %I ssp
            JOIN
                market_mk12_deal_pipeline dp ON ssp.sp_id = dp.sp_id AND ssp.sector_num = dp.sector
            WHERE
                ssp.task_id_move_storage = $1', sealing_table);
    ELSE
        RAISE EXCEPTION 'Invalid sealing_table name: %', sealing_table;
    END IF;

    -- Execute the dynamic SQL query with the task_id parameter
FOR pms IN EXECUTE query USING task_id
    LOOP
        -- Update the market_mk12_deal_pipeline table with the reg_seal_proof and indexing_created_at values
    UPDATE market_mk12_deal_pipeline
    SET
        reg_seal_proof = pms.reg_seal_proof,
        indexing_created_at = NOW() AT TIME ZONE 'UTC'
    WHERE
        uuid = pms.uuid;
    END LOOP;

    -- If everything is successful, simply exit
    RETURN;

EXCEPTION
    WHEN OTHERS THEN
        -- Rollback the transaction and raise the exception for Go to catch
        ROLLBACK;
        RAISE EXCEPTION 'Failed to create indexing task: %', SQLERRM;
END;
$$ LANGUAGE plpgsql;

-- This table can be used to track remote piece for offline deals
-- The entries must be created by users. Entry is removed when deal is
-- removed from market_mk12_deal_pipeline table using a key constraint
CREATE TABLE market_offline_urls (
    uuid TEXT NOT NULL,

    url TEXT NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',

    raw_size BIGINT NOT NULL,

    CONSTRAINT market_offline_urls_uuid_fk FOREIGN KEY (uuid)
        REFERENCES market_mk12_deal_pipeline (uuid)
        ON DELETE CASCADE,
    CONSTRAINT market_offline_urls_uuid_unique UNIQUE (uuid)
);

-- This table is used for coordinating libp2p nodes
CREATE TABLE libp2p (
    sp_id BIGINT NOT NULL PRIMARY KEY,
    priv_key BYTEA NOT NULL,
    running_on TEXT DEFAULT NULL,
    updated_at TIMESTAMPTZ DEFAULT NULL
);

-- -- Function used to update the libp2p table
CREATE OR REPLACE FUNCTION update_libp2p_node(_running_on TEXT)
RETURNS VOID AS $$
DECLARE
current_running_on TEXT;
    last_updated TIMESTAMPTZ;
BEGIN
    -- Fetch the current values of running_on and updated_at
    SELECT running_on, updated_at INTO current_running_on, last_updated
    FROM libp2p
    WHERE running_on IS NOT NULL
        LIMIT 1;

    -- If running_on is already set
    IF current_running_on IS NOT NULL THEN
            -- Check if updated_at is more than 5 minutes old
            IF last_updated < NOW() - INTERVAL '5 minutes' THEN
                -- Update running_on and updated_at
                UPDATE libp2p
                SET running_on = _running_on,
                    updated_at = NOW() AT TIME ZONE 'UTC'
                WHERE running_on = current_running_on;
            ELSE
                -- Raise an exception if the node was updated within the last 5 minutes
                RAISE EXCEPTION 'Libp2p node already running on "%"', current_running_on;
            END IF;
    ELSE
            -- If running_on is NULL, set it and update the timestamp
            UPDATE libp2p
            SET running_on = _running_on,
                updated_at = NOW() AT TIME ZONE 'UTC'
            WHERE running_on IS NULL;
    END IF;
END;
$$ LANGUAGE plpgsql;


-- Table for old lotus market deals. This is just for deal
-- which are still alive. It should not be used for any processing
CREATE TABLE market_legacy_deals (
    signed_proposal_cid TEXT  NOT NULL,
    sp_id BIGINT  NOT NULL,
    client_peer_id TEXT NOT NULL,

    proposal_signature BYTEA  NOT NULL,
    proposal jsonb  NOT NULL,

    piece_cid TEXT  NOT NULL,
    piece_size BIGINT  NOT NULL,

    verified BOOLEAN  NOT NULL,

    start_epoch BIGINT  NOT NULL,
    end_epoch BIGINT  NOT NULL,

    publish_cid TEXT  NOT NULL,
    chain_deal_id BIGINT  NOT NULL,

    fast_retrieval BOOLEAN  NOT NULL,

    created_at TIMESTAMPTZ  NOT NULL,
    sector_num BIGINT  NOT NULL,

    primary key (sp_id, piece_cid, signed_proposal_cid)
);

-- Table for DDO deals in Boost
CREATE TABLE market_direct_deals (
    uuid TEXT NOT NULL,
    sp_id BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    client TEXT NOT NULL,

    offline BOOLEAN NOT NULL,
    verified BOOLEAN NOT NULL,

    start_epoch BIGINT NOT NULL,
    end_epoch BIGINT NOT NULL,

    allocation_id BIGINT NOT NULL,

    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL,

    fast_retrieval BOOLEAN NOT NULL,
    announce_to_ipni BOOLEAN NOT NULL,

    unique (uuid)
);



