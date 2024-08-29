-- Table for storing IPNI ads
CREATE TABLE ipni (
    order_number BIGSERIAL PRIMARY KEY, -- Unique increasing order number
    ad_cid TEXT NOT NULL,
    context_id TEXT NOT NULL, -- abi.PieceInfo in Curio
    -- metadata column in not required as Curio only supports one type of metadata(HTTP)
    is_rm BOOLEAN NOT NULL,

    previous TEXT, -- previous ad will only be null for first ad in chain

    provider TEXT NOT NULL, -- peerID from libp2p, this is main identifier on IPNI side
    addresses TEXT NOT NULL, -- HTTP retrieval server addresses

    signature BYTEA NOT NULL,
    entries TEXT NOT NULL, -- CID of first link in entry chain

    unique (ad_cid)
);

-- This index will help speed up the lookup of all ads for a specific provider and ensure fast ordering by order_number
CREATE INDEX ipni_provider_order_number ON ipni(provider, order_number);

-- This index will speed up lookups based on the ad_cid, which is frequently used to identify specific ads
CREATE UNIQUE INDEX ipni_ad_cid ON ipni(ad_cid);

-- This index will speed up lookups based on the ad_cid, which is frequently used to identify specific ads
CREATE UNIQUE INDEX ipni_context_id ON ipni(context_id, ad_cid, is_rm);

-- Since the get_ad_chain function relies on both provider and ad_cid to find the order_number, this index will optimize that query:
CREATE INDEX ipni_provider_ad_cid ON ipni(provider, ad_cid);


CREATE TABLE ipni_head (
    provider TEXT NOT NULL PRIMARY KEY, -- PeerID from libp2p, this is the main identifier
    head TEXT NOT NULL, -- ad_cid from the ipni table, representing the head of the ad chain

    FOREIGN KEY (head) REFERENCES ipni(ad_cid) ON DELETE RESTRICT -- Prevents deletion if it's referenced
);

CREATE OR REPLACE FUNCTION insert_ad_and_update_head(
    _ad_cid TEXT,
    _context_id TEXT,
    _is_rm BOOLEAN,
    _provider TEXT,
    _addresses TEXT,
    _signature BYTEA,
    _entries TEXT
) RETURNS VOID AS $$
DECLARE
_previous TEXT;
BEGIN
    -- Determine the previous ad_cid in the chain for this provider
    SELECT head INTO _previous
    FROM ipni_head
    WHERE provider = _provider;

    -- Insert the new ad into the ipni table with an automatically assigned order_number
    INSERT INTO ipni (ad_cid, context_id, is_rm, previous, provider, addresses, signature, entries)
    VALUES (_ad_cid, _context_id, _is_rm, _previous, _provider, _addresses, _signature, _entries);

    -- Update the ipni_head table to set the new ad as the head of the chain
    INSERT INTO ipni_head (provider, head)
    VALUES (_provider, _ad_cid)
        ON CONFLICT (provider) DO UPDATE SET head = EXCLUDED.head;
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION get_ad_chain(
    _provider TEXT,
    _ad_cid TEXT
) RETURNS TABLE (
    ad_cid TEXT,
    context_id TEXT,
    is_rm BOOLEAN,
    previous TEXT,
    provider TEXT,
    addresses TEXT,
    signature BYTEA,
    entries TEXT,
    order_number BIGINT
) AS $$
DECLARE
_order_number BIGINT;
BEGIN
    -- Get the order_number for the specified ad_cid and provider
    SELECT order_number INTO _order_number
    FROM ipni
    WHERE ad_cid = _ad_cid AND provider = _provider;

    -- Return all ads from the head to the specified order_number
    RETURN QUERY
    SELECT ad_cid, context_id, is_rm, previous, provider, addresses, signature, entries, order_number
    FROM ipni
    WHERE provider = _provider AND order_number <= _order_number
    ORDER BY order_number ASC;
END;
$$ LANGUAGE plpgsql;

-- IPNI pipeline is kept separate from rest for robustness
-- and reuse. This allows for removing, recreating as using CLI.
CREATE TABLE ipni_task (
    sp_id BIGINT NOT NULL,
    sector BIGINT NOT NULL,
    reg_seal_proof INT NOT NULL,
    sector_offset BIGINT,

    context_id BYTEA NOT NULL PRIMARY KEY,
    is_rm BOOLEAN NOT NULL,

    provider TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    task_id BIGINT DEFAULT NULL,
    complete BOOLEAN DEFAULT FALSE,

    PRIMARY KEY (provider, context_id, is_rm)
);

-- Function to create ipni tasks
CREATE OR REPLACE FUNCTION insert_ipni_task(
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
            INSERT INTO ipni_task (sp_id, sector, reg_seal_proof, sector_offset, provider, context_id, is_rm, created_at, task_id, complete)
            VALUES (_sp_id, _sector, _reg_seal_proof, _sector_offset, _provider, _context_id, _is_rm, TIMEZONE('UTC', NOW()), _task_id, FALSE);
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
    INSERT INTO ipni_task (sp_id, sector, reg_seal_proof, sector_offset, provider, context_id, is_rm, created_at, task_id, complete)
    VALUES (_sp_id, _sector, _reg_seal_proof, _sector_offset, _provider, _context_id, _is_rm, TIMEZONE('UTC', NOW()), _task_id, FALSE);
END;
$$ LANGUAGE plpgsql;

