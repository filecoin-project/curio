-- Add raw_size column to mk12 deals to calculate pieceCidV2
ALTER TABLE market_mk12_deals
    ADD COLUMN raw_size BIGINT;

-- Add raw_size column to mk12-ddo deals to calculate pieceCidV2
ALTER TABLE market_direct_deals
    ADD COLUMN raw_size BIGINT;

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
    ADD PRIMARY KEY (id, sp_id, piece_cid, piece_length);

-- Add a column to relate a piece park piece to mk20 deal
ALTER TABLE market_piece_deal
ADD COLUMN piece_ref BIGINT;

-- Allow piece_offset to be null for PDP deals
ALTER TABLE market_piece_deal
    ALTER COLUMN piece_offset DROP NOT NULL;

-- Add column to skip scheduling piece_park. Used for upload pieces
ALTER TABLE parked_pieces
    ADD COLUMN skip BOOLEAN NOT NULL DEFAULT FALSE;

-- Add column piece_cid_v2 to IPNI table
ALTER TABLE ipni
    ADD COLUMN piece_cid_v2 TEXT;

-- Add metadata column to IPNI table which defaults to binary of IpfsGatewayHttp
ALTER TABLE ipni
    ADD COLUMN metadata BYTEA NOT NULL DEFAULT '\xa01200';

-- The order_number column must be completely sequential
ALTER SEQUENCE ipni_order_number_seq CACHE 1;

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
     ) ON CONFLICT (id, sp_id, piece_cid, piece_length) DO NOTHING;

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


-- Update raw_size for existing deals (One time backfill migration)
BEGIN;
    UPDATE market_mk12_deals d
    SET raw_size = mpd.raw_size
        FROM market_piece_deal mpd
    WHERE d.uuid = mpd.id;

    UPDATE market_direct_deals d
    SET raw_size = mpd.raw_size
        FROM market_piece_deal mpd
    WHERE d.uuid = mpd.id;

    UPDATE market_mk12_deals d
    SET raw_size = p.raw_size
        FROM market_mk12_deal_pipeline p
    WHERE d.uuid = p.uuid
      AND d.raw_size IS NULL
      AND p.raw_size IS NOT NULL;

    UPDATE market_direct_deals d
    SET raw_size = p.raw_size
        FROM market_mk12_deal_pipeline p
    WHERE d.uuid = p.uuid
      AND d.raw_size IS NULL
      AND p.raw_size IS NOT NULL;
COMMIT;

-- This is main MK20 Deal table. Rows are added per deal and some
-- modification is allowed later
CREATE TABLE market_mk20_deal (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT PRIMARY KEY,
    client TEXT NOT NULL,

    piece_cid_v2 TEXT,

    data JSONB NOT NULL DEFAULT 'null',

    ddo_v1 JSONB NOT NULL DEFAULT 'null',
    retrieval_v1 JSONB NOT NULL DEFAULT 'null',
    pdp_v1 JSONB NOT NULL DEFAULT 'null'
);
COMMENT ON COLUMN market_mk20_deal.id IS 'This is ULID TEXT';
COMMENT ON COLUMN market_mk20_deal.client IS 'Client must always be text as this can be a non Filecoin address like ed25519';

-- This is main pipeline table for PoRep processing of MK20 deals
CREATE TABLE market_mk20_pipeline (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    id TEXT NOT NULL,
    sp_id BIGINT NOT NULL,
    contract TEXT NOT NULL,
    client TEXT NOT NULL,
    piece_cid_v2 TEXT NOT NULL,
    piece_cid TEXT NOT NULL, -- This is pieceCid V1 to allow easy table lookups
    piece_size BIGINT NOT NULL,
    raw_size BIGINT NOT NULL,
    offline BOOLEAN NOT NULL,
    url TEXT DEFAULT NULL,
    indexing BOOLEAN NOT NULL,
    announce BOOLEAN NOT NULL,
    allocation_id BIGINT DEFAULT NULL,
    duration BIGINT NOT NULL,
    piece_aggregation INT NOT NULL DEFAULT 0, -- This is set when user sends a aggregated piece. It is also set as `deal_aggregation` when deal is aggregated on SP side.

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
COMMENT ON COLUMN market_mk20_pipeline.piece_aggregation IS 'This is set when user sends a aggregated piece. It is also set as `deal_aggregation` when deal is aggregated on SP side.';
COMMENT ON COLUMN market_mk20_pipeline.deal_aggregation IS 'This is set when user sends a deal with aggregated source. This value is passed to piece_aggregation when aggregation is finished and a single piece remains';

-- This table is used to hold MK20 deals waiting for PoRep pipeline
-- to process. This allows disconnecting the need to immediately process
-- deals as received and allow upload later strategy to work
CREATE TABLE market_mk20_pipeline_waiting (
    id TEXT PRIMARY KEY
);

-- This table is used to keep track of deals which need data upload.
-- A separate table helps easier status check, chunked+serial upload support
CREATE TABLE market_mk20_upload_waiting (
    id TEXT PRIMARY KEY,
    chunked BOOLEAN DEFAULT NULL,
    ref_id BIGINT DEFAULT NULL,
    ready_at TIMESTAMPTZ DEFAULT NULL
);

-- This table help disconnected downloads from main PoRep/PDP pipelines
-- It helps with allowing multiple downloads per deal i.e. server side aggregation.
-- This also allows us to reuse ongoing downloads within the same deal aggregation.
-- It also allows using a common download pipeline for both PoRep and PDP.
CREATE TABLE market_mk20_download_pipeline (
    id TEXT NOT NULL,
    product TEXT NOT NULL, -- This allows us to run multiple refs per product for easier lifecycle management
    piece_cid_v2 TEXT NOT NULL,
    ref_ids BIGINT[] NOT NULL,
    PRIMARY KEY (id, product, piece_cid_v2)
);

-- Offline URLs for PoRep deals.
CREATE TABLE market_mk20_offline_urls (
    id TEXT NOT NULL,
    piece_cid_v2 TEXT NOT NULL,
    url TEXT NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}',
    PRIMARY KEY (id, piece_cid_v2)
);

-- This table tracks the chunk upload progress for a MK20 deal. Common for both
-- PoRep and PDP
CREATE TABLE market_mk20_deal_chunk (
    id TEXT not null,
    chunk INT not null,
    chunk_size BIGINT not null,
    ref_id BIGINT DEFAULT NULL,
    complete BOOLEAN DEFAULT FALSE,
    completed_at TIMESTAMPTZ,
    finalize BOOLEAN DEFAULT FALSE,
    finalize_task_id BIGINT DEFAULT NULL,
    PRIMARY KEY (id, chunk)
);

-- MK20 product and their status table
CREATE TABLE market_mk20_products (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

-- MK20 supported data sources and their status table
CREATE TABLE market_mk20_data_source (
    name TEXT PRIMARY KEY,
    enabled BOOLEAN DEFAULT TRUE
);

-- Add products and data sources to table
INSERT INTO market_mk20_products (name, enabled) VALUES ('ddo_v1', TRUE);
INSERT INTO market_mk20_products (name, enabled) VALUES ('retrieval_v1', TRUE);
INSERT INTO market_mk20_products (name, enabled) VALUES ('pdp_v1', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('http', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('aggregate', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('offline', TRUE);
INSERT INTO market_mk20_data_source (name, enabled) VALUES ('put', TRUE);

-- This function sets an upload completion time. It is used to removed
-- upload for deal which are not finalized in 1 hour so we don't waste space.
CREATE OR REPLACE FUNCTION set_ready_at_for_serial_upload()
RETURNS TRIGGER AS $$
BEGIN
    -- Transition into "serial ready" state: chunked=false AND ref_id IS NOT NULL
    IF NEW.chunked IS FALSE
    AND NEW.ref_id IS NOT NULL
    AND OLD.ready_at IS NULL
    AND NOT (OLD.chunked IS FALSE AND OLD.ref_id IS NOT NULL) THEN
        NEW.ready_at := NOW() AT TIME ZONE 'UTC';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_ready_at_serial
    BEFORE UPDATE OF ref_id, chunked ON market_mk20_upload_waiting
    FOR EACH ROW
    EXECUTE FUNCTION set_ready_at_for_serial_upload();

-- This function sets an upload completion time. It is used to removed
-- upload for deal which are not finalized in 1 hour so we don't waste space.
CREATE OR REPLACE FUNCTION set_ready_at_when_all_chunks_complete()
RETURNS TRIGGER AS $$
BEGIN
  -- Only react when a chunk transitions to complete = true
    IF (TG_OP = 'UPDATE' OR TG_OP = 'INSERT') AND NEW.complete IS TRUE THEN
        -- If no incomplete chunks remain, set ready_at once
        IF NOT EXISTS (
          SELECT 1 FROM market_mk20_deal_chunk
          WHERE id = NEW.id AND (complete IS NOT TRUE)
        ) THEN
        UPDATE market_mk20_upload_waiting
        SET ready_at = NOW() AT TIME ZONE 'UTC'
        WHERE id = NEW.id
          AND chunked = true
          AND ready_at IS NULL;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;


CREATE TRIGGER trg_ready_at_chunks_update
    AFTER INSERT OR UPDATE OF complete ON market_mk20_deal_chunk
    FOR EACH ROW
    EXECUTE FUNCTION set_ready_at_when_all_chunks_complete();

-- This function triggers a download for an offline piece.
-- It is different from MK1.2 PoRep pipeline as it downloads the offline pieces
-- locally. This is to allow serving retrievals with piece park.
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

-- Main DataSet table for PDP
CREATE TABLE pdp_data_set (
    id BIGINT PRIMARY KEY, -- on-chain dataset id
    client TEXT NOT NULL, -- client wallet which requested this dataset

    -- updated when a challenge is requested (either by first dataset add or by invokes of nextProvingPeriod)
    -- initially NULL on fresh dataset
    prev_challenge_request_epoch BIGINT,

    -- task invoking nextProvingPeriod, the task should be spawned any time prove_at_epoch+challenge_window is in the past
    challenge_request_task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,

    -- nextProvingPeriod message hash, when the message lands prove_task_id will be spawned and
    -- this value will be set to NULL
    challenge_request_msg_hash TEXT,

    -- the proving period for this proofset and the challenge window duration
    proving_period BIGINT,
    challenge_window BIGINT,

    -- the epoch at which the next challenge window starts and proofs can be submitted
    -- initialized to NULL indicating a special proving period init task handles challenge generation
    prove_at_epoch BIGINT,

    -- flag indicating that the proving period is ready for init.  Currently set after first add
    -- Set to true after first root add
    init_ready BOOLEAN NOT NULL DEFAULT FALSE,

    create_deal_id TEXT NOT NULL, -- mk20 deal ID for creating this data_set
    create_message_hash TEXT NOT NULL,

    removed BOOLEAN DEFAULT FALSE,

    remove_deal_id TEXT DEFAULT NULL, -- mk20 deal ID for removing this data_set
    remove_message_hash TEXT DEFAULT NULL,

    unique (create_deal_id),
    unique (remove_deal_id)
);

-- DataSet create table governs the DataSet create task
CREATE TABLE pdp_data_set_create (
    id TEXT PRIMARY KEY, -- This is Market V2 Deal ID for lookup and response
    client TEXT NOT NULL,

    record_keeper TEXT NOT NULL,
    extra_data BYTEA,

    task_id BIGINT DEFAULT NULL,
    tx_hash TEXT DEFAULT NULL
);

-- DataSet delete table governs the DataSet delete task
CREATE TABLE pdp_data_set_delete (
    id TEXT PRIMARY KEY, -- This is Market V2 Deal ID for lookup and response
    client TEXT NOT NULL,

    set_id BIGINT NOT NULL,
    extra_data BYTEA,

    task_id BIGINT DEFAULT NULL,
    tx_hash TEXT DEFAULT NULL
);

-- This table governs the delete piece tasks
CREATE TABLE pdp_piece_delete (
    id TEXT PRIMARY KEY, -- This is Market V2 Deal ID for lookup and response
    client TEXT NOT NULL,

    set_id BIGINT NOT NULL,
    pieces BIGINT[] NOT NULL,
    extra_data BYTEA,

    task_id BIGINT DEFAULT NULL,
    tx_hash TEXT DEFAULT NULL
);

-- Main DataSet Piece table. Any and all pieces ever added by SP must be part of this table
CREATE TABLE pdp_dataset_piece (
    data_set_id BIGINT NOT NULL, -- pdp_data_sets.id
    client TEXT NOT NULL,

    piece_cid_v2 TEXT NOT NULL, -- root cid (piececid v2)

    piece BIGINT DEFAULT NULL, -- on-chain index of the piece in the pieceCids sub-array

    piece_ref BIGINT NOT NULL, -- piece_ref_id

    add_deal_id TEXT NOT NULL, -- mk20 deal ID for adding this root to dataset
    add_message_hash TEXT NOT NULL,
    add_message_index BIGINT NOT NULL, -- index of root in the add message

    removed BOOLEAN DEFAULT FALSE,
    remove_deal_id TEXT DEFAULT NULL, -- mk20 deal ID for removing this root from dataset
    remove_message_hash TEXT DEFAULT NULL,
    remove_message_index BIGINT DEFAULT NULL,

    PRIMARY KEY (data_set_id, piece)
);

CREATE TABLE pdp_pipeline (
    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    id TEXT NOT NULL,
    client TEXT NOT NULL,

    piece_cid_v2 TEXT NOT NULL, -- v2 piece_cid

    data_set_id BIGINT NOT NULL,

    extra_data BYTEA,

    piece_ref BIGINT DEFAULT NULL,

    downloaded BOOLEAN DEFAULT FALSE,

    commp_task_id BIGINT DEFAULT NULL,
    after_commp BOOLEAN DEFAULT FALSE,

    deal_aggregation INT NOT NULL DEFAULT 0,
    aggr_index BIGINT DEFAULT 0,
    agg_task_id BIGINT DEFAULT NULL,
    aggregated BOOLEAN DEFAULT FALSE,

    add_piece_task_id BIGINT DEFAULT NULL,
    after_add_piece BOOLEAN DEFAULT FALSE,

    add_message_hash TEXT,
    add_message_index BIGINT NOT NULL DEFAULT 0, -- index of root in the add message

    after_add_piece_msg BOOLEAN DEFAULT FALSE,

    save_cache_task_id BIGINT DEFAULT NULL,
    after_save_cache BOOLEAN DEFAULT FALSE,

    indexing BOOLEAN DEFAULT FALSE,
    indexing_created_at TIMESTAMPTZ DEFAULT NULL,
    indexing_task_id BIGINT DEFAULT NULL,
    indexed BOOLEAN DEFAULT FALSE,

    announce BOOLEAN DEFAULT FALSE,
    announce_payload BOOLEAN DEFAULT FALSE,

    announced BOOLEAN DEFAULT FALSE,
    announced_payload BOOLEAN DEFAULT FALSE,

    complete BOOLEAN DEFAULT FALSE,

    PRIMARY KEY (id, aggr_index)
);

-- This function is used to mark a piece as downloaded in pdp_pipeline
-- A deal with multiple HTTP sources will have multiple ref_ids,
-- and download is handled by market_mk20_download_pipeline table
-- We add ref_id to pdp_pipeline once download is successful.
create or replace function mk20_pdp_mark_downloaded(_product text)
returns integer
language plpgsql
as $$
declare
    updated_count int := 0;
begin
    with candidates as (
        select p.id, p.piece_cid_v2, dp.ref_ids
        from pdp_pipeline p
        join market_mk20_download_pipeline dp
          on dp.id = p.id
          and dp.piece_cid_v2 = p.piece_cid_v2
          and dp.product = _product
        where p.piece_ref is null
    ),
    picked as (
        -- choose ONE completed ref_id from the array for each (id,piece_cid_v2)
        select c.id, c.piece_cid_v2, c.ref_ids, ch.ref_id as chosen_ref
        from candidates c
        cross join lateral (
            select pr.ref_id
            from unnest(c.ref_ids) as r(ref_id)
            join parked_piece_refs pr on pr.ref_id = r.ref_id
            join parked_pieces pp on pp.id = pr.piece_id
            where pp.complete = true
            limit 1
        ) ch
    ),
    del_other_refs as (
        delete from parked_piece_refs pr
        using picked
        where pr.ref_id = any(picked.ref_ids)
            and pr.ref_id != picked.chosen_ref
        returning 1
    ),
    del_download_rows as (
        delete from market_mk20_download_pipeline dp
        using picked
        where dp.id = picked.id
            and dp.piece_cid_v2 = picked.piece_cid_v2
            and dp.product = _product
        returning 1
    ),
    upd as (
        update pdp_pipeline p
        set downloaded = true,
            piece_ref  = picked.chosen_ref
        from picked
        where p.id = picked.id
            and p.piece_cid_v2 = picked.piece_cid_v2
        returning 1
    )
    select count(*) into updated_count from upd;

    return updated_count;
end;
$$;

CREATE TABLE market_mk20_clients (
    client TEXT PRIMARY KEY,
    allowed BOOLEAN DEFAULT TRUE
);

CREATE TABLE pdp_proving_tasks (
    data_set_id BIGINT NOT NULL, -- pdp_data_set.id
    task_id BIGINT NOT NULL, -- harmony_task task ID

    PRIMARY KEY (data_set_id, task_id),
    FOREIGN KEY (data_set_id) REFERENCES pdp_data_set(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE CASCADE
);

-- IPNI pipeline is kept separate from rest for robustness
-- and reuse. This allows for removing, recreating ads using CLI.
CREATE TABLE pdp_ipni_task (
    context_id BYTEA NOT NULL,
    is_rm BOOLEAN NOT NULL,

    id TEXT NOT NULL,

    provider TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    task_id BIGINT DEFAULT NULL,
    complete BOOLEAN DEFAULT FALSE,

    PRIMARY KEY (context_id, is_rm)
);

-- Function to create ipni tasks
CREATE OR REPLACE FUNCTION insert_pdp_ipni_task(
    _context_id BYTEA,
    _is_rm BOOLEAN,
    _id TEXT,
    _provider TEXT,
    _task_id BIGINT DEFAULT NULL
) RETURNS VOID AS $$
DECLARE
    _existing_is_rm BOOLEAN;
    _latest_is_rm BOOLEAN;
BEGIN
    -- Check if ipni_task has the same context_id and provider with a different is_rm value
    SELECT is_rm INTO _existing_is_rm
    FROM pdp_ipni_task
    WHERE provider = _provider AND context_id = _context_id AND is_rm != _is_rm
    LIMIT 1;

    -- If a different is_rm exists for the same context_id and provider, insert the new task
    IF FOUND THEN
        INSERT INTO pdp_ipni_task (context_id, is_rm, id, provider, task_id, created_at)
        VALUES (_context_id, _is_rm, _id, _provider, _task_id);
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
    INSERT INTO pdp_ipni_task (context_id, is_rm, id, provider, task_id)
    VALUES (_context_id, _is_rm, _id, _provider, _task_id);
END;
$$ LANGUAGE plpgsql;

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
    -- Determine the previous ad_cid in the chain for this provider
    SELECT head INTO _previous
    FROM ipni_head
    WHERE provider = _provider;

    -- Insert the new ad into the ipni table with an automatically assigned order_number
    INSERT INTO ipni (ad_cid, context_id, metadata, is_rm, previous, provider, addresses, signature, entries, piece_cid_v2, piece_cid, piece_size)
    VALUES (_ad_cid, _context_id, _metadata, _is_rm, _previous, _provider, _addresses, _signature, _entries, _piece_cid_v2, _piece_cid, _piece_size);

    -- Update the ipni_head table to set the new ad as the head of the chain
    INSERT INTO ipni_head (provider, head)
    VALUES (_provider, _ad_cid)
        ON CONFLICT (provider) DO UPDATE SET head = EXCLUDED.head;

END;
$$ LANGUAGE plpgsql;


CREATE TABLE piece_cleanup (
    id TEXT NOT NULL,
    piece_cid_v2 TEXT NOT NULL,
    pdp BOOLEAN NOT NULL,

    task_id BIGINT,

    PRIMARY KEY (id, pdp)
);

-- This functions remove the row from market_piece_deal and then goes on to
-- clean up market_piece_metadata and parked_piece_refs as required
CREATE OR REPLACE FUNCTION remove_piece_deal(
    _id           TEXT,
    _sp_id        BIGINT,
    _piece_cid    TEXT,
    _piece_length BIGINT
) RETURNS VOID AS $$
DECLARE
    v_piece_ref   BIGINT;
    v_remaining   BIGINT;
BEGIN
    -- 1) Delete the exact deal row and capture piece_ref
    DELETE FROM market_piece_deal
    WHERE id = _id
      AND sp_id = _sp_id
      AND piece_cid = _piece_cid
      AND piece_length = _piece_length
        RETURNING piece_ref
    INTO v_piece_ref;

    IF NOT FOUND THEN
        RAISE EXCEPTION
          'market_piece_deal not found for id=%, sp_id=%, piece_cid=%, piece_length=%',
          _id, _sp_id, _piece_cid, _piece_length;
    END IF;

    -- 2) If no other deals reference the same piece, remove metadata
    SELECT COUNT(*)
    INTO v_remaining
    FROM market_piece_deal
    WHERE piece_cid = _piece_cid
      AND piece_length = _piece_length;

    IF v_remaining = 0 THEN
        DELETE FROM market_piece_metadata
        WHERE piece_cid  = _piece_cid
          AND piece_size = _piece_length;
        -- (DELETE is idempotent even if no row exists)
    END IF;

    -- 3) If present, remove the parked piece reference
    IF v_piece_ref IS NOT NULL THEN
        DELETE FROM parked_piece_refs
        WHERE ref_id = v_piece_ref;
        -- (FKs from pdp_* tables will cascade/SET NULL per their definitions)
    END IF;
END;
$$ LANGUAGE plpgsql;


create or replace function mk20_ddo_mark_downloaded(_product text)
returns integer
language plpgsql
as $$
declare
updated_count int := 0;
begin
    with candidates as (
        select p.id, p.piece_cid_v2, dp.ref_ids
        from market_mk20_pipeline p
        join market_mk20_download_pipeline dp
          on dp.id = p.id
              and dp.piece_cid_v2 = p.piece_cid_v2
              and dp.product = _product
        where p.url is null
    ),
    picked as (
        -- choose ONE completed ref_id from the array for each (id,piece_cid_v2)
        select c.id, c.piece_cid_v2, c.ref_ids, ch.ref_id as chosen_ref
        from candidates c
        cross join lateral (
            select pr.ref_id
            from unnest(c.ref_ids) as r(ref_id)
            join parked_piece_refs pr on pr.ref_id = r.ref_id
            join parked_pieces pp on pp.id = pr.piece_id
            where pp.complete = true
            limit 1
        ) ch
    ),
    del_other_refs as (
        delete from parked_piece_refs pr
        using picked
        where pr.ref_id = any(picked.ref_ids)
        and pr.ref_id != picked.chosen_ref
        returning 1
    ),
    del_download_rows as (
        delete from market_mk20_download_pipeline dp
        using picked
        where dp.id = picked.id
        and dp.piece_cid_v2 = picked.piece_cid_v2
        and dp.product = _product
        returning 1
    ),
    upd as (
        update market_mk20_pipeline p
        set downloaded = true,
            url        = 'pieceref:' || picked.chosen_ref::text
        from picked
        where p.id = picked.id
        and p.piece_cid_v2 = picked.piece_cid_v2
        returning 1
    )
    select count(*) into updated_count from upd;

    return updated_count;
end;
$$;


