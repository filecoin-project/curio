-- Piece Park adjustments

ALTER TABLE parked_pieces ADD COLUMN long_term BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE parked_pieces DROP CONSTRAINT IF EXISTS parked_pieces_piece_cid_key;
ALTER TABLE parked_pieces ADD CONSTRAINT parked_pieces_piece_cid_cleanup_task_id_key UNIQUE (piece_cid, piece_padded_size, long_term, cleanup_task_id);

ALTER TABLE parked_piece_refs ADD COLUMN long_term BOOLEAN NOT NULL DEFAULT FALSE;

-- PDP tables
-- PDP services authenticate with ecdsa-sha256 keys; Allowed services here
CREATE TABLE pdp_services (
    id BIGSERIAL PRIMARY KEY,
    pubkey BYTEA NOT NULL,

    -- service_url TEXT NOT NULL,
    service_label TEXT NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(pubkey),
    UNIQUE(service_label)
);

CREATE TABLE pdp_piece_uploads (
    id UUID PRIMARY KEY NOT NULL,
    service TEXT NOT NULL, -- pdp_services.id

    check_hash_codec TEXT NOT NULL, -- hash multicodec used for checking the piece
    check_hash BYTEA NOT NULL, -- hash of the piece
    check_size BIGINT NOT NULL, -- size of the piece

    piece_cid TEXT, -- piece cid v2
    notify_url TEXT NOT NULL, -- URL to notify when piece is ready

    notify_task_id BIGINT, -- harmonytask task ID, moves to pdp_piecerefs and calls notify_url when piece is ready

    piece_ref BIGINT, -- packed_piece_refs.ref_id

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    FOREIGN KEY (piece_ref) REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL
);

-- PDP piece references, this table tells Curio which pieces in storage are managed by PDP
CREATE TABLE pdp_piecerefs (
    id BIGSERIAL PRIMARY KEY,
    service TEXT NOT NULL, -- pdp_services.id
    piece_cid TEXT NOT NULL, -- piece cid v2
    piece_ref BIGINT NOT NULL, -- parked_piece_refs.ref_id
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    proofset_refcount BIGINT NOT NULL DEFAULT 0, -- maintained by triggers

    UNIQUE(piece_ref),
    FOREIGN KEY (service) REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    FOREIGN KEY (piece_ref) REFERENCES parked_piece_refs(ref_id) ON DELETE CASCADE
);

-- PDP hash to piece cid mapping
CREATE TABLE pdp_piece_mh_to_commp (
    mhash BYTEA PRIMARY KEY,
    size BIGINT NOT NULL,
    commp TEXT NOT NULL
);

CREATE INDEX pdp_piecerefs_piece_cid_idx ON pdp_piecerefs(piece_cid);

-- PDP proofsets we maintain

CREATE TABLE pdp_proof_sets (
    id BIGINT PRIMARY KEY, -- on-chain proofset id

    -- updated when a challenge is requested (either by first proofset add or by invokes of nextProvingPeriod)
    -- initially NULL on fresh proofsets.
    prev_challenge_request_epoch BIGINT,

    -- task invoking nextProvingPeriod, the task should be spawned any time prove_at_epoch+challenge_window is in the past
    challenge_request_task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,

    -- nextProvingPeriod message hash, when the message lands prove_task_id will be spawned and
    -- this value will be set to NULL
    challenge_request_msg_hash TEXT,

    -- the proving period for this proofset and the challenge window duration
    proving_period BIGINT NOT NULL,
    challenge_window BIGINT NOT NULL,

    -- the epoch at which the next challenge window starts and proofs can be submitted
    -- initialized to NULL indicating a special proving period init task handles challenge generation
    prove_at_epoch BIGINT,

    -- flag indicating that the proving period is ready for init.  Currently set after first add 
    -- Set to true after first root add
    init_ready BOOLEAN,

    create_message_hash TEXT NOT NULL,
    service TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE RESTRICT
);

CREATE TABLE pdp_prove_tasks (
    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
    task_id BIGINT NOT NULL, -- harmonytask task ID

    PRIMARY KEY (proofset, task_id),
    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE,
    FOREIGN KEY (task_id) REFERENCES harmony_task(id) ON DELETE CASCADE
);

-- proofset creation requests
CREATE TABLE pdp_proofset_creates (
    create_message_hash TEXT PRIMARY KEY REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,

    -- NULL if not yet processed, TRUE if processed and successful, FALSE if processed and failed
    -- NOTE: ok is maintained by a trigger below
    ok BOOLEAN DEFAULT NULL,

    proofset_created BOOLEAN NOT NULL DEFAULT FALSE, -- set to true when the proofset is created

    service TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE CASCADE, -- service that requested the proofset
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- proofset roots
CREATE TABLE pdp_proofset_roots (
    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
    root TEXT NOT NULL, -- root cid (piececid v2)

    add_message_hash TEXT NOT NULL REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_index BIGINT NOT NULL, -- index of root in the add message

    root_id BIGINT NOT NULL, -- on-chain index of the root in the rootCids sub-array

    -- aggregation roots (aggregated like pieces in filecoin sectors)
    subroot TEXT NOT NULL, -- subroot cid (piececid v2), with no aggregation this == root
    subroot_offset BIGINT NOT NULL, -- offset of the subroot in the root
    subroot_size BIGINT NOT NULL, -- size of the subroot (padded piece size)

    pdp_pieceref BIGINT NOT NULL, -- pdp_piecerefs.id

    CONSTRAINT pdp_proofset_roots_root_id_unique PRIMARY KEY (proofset, root_id, subroot_offset),

    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE, -- cascade, if we drop a proofset, we no longer care about the roots
    FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE SET NULL -- sets null on delete so that it's easy to notice and clean up
);

-- proofset root adds - tracking add-root messages which didn't land yet, so don't have a known root_id
CREATE TABLE pdp_proofset_root_adds (
    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
    root TEXT NOT NULL, -- root cid (piececid v2)

    add_message_hash TEXT NOT NULL REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_ok BOOLEAN, -- set to true when the add message is processed
    add_message_index BIGINT NOT NULL, -- index of root in the add message

    -- aggregation roots (aggregated like pieces in filecoin sectors)
    subroot TEXT NOT NULL, -- subroot cid (piececid v2), with no aggregation this == root
    subroot_offset BIGINT NOT NULL, -- offset of the subroot in the root (padded byte offset)
    subroot_size BIGINT NOT NULL, -- size of the subroot (padded piece size)

    pdp_pieceref BIGINT NOT NULL, -- pdp_piecerefs.id

    CONSTRAINT pdp_proofset_root_adds_root_id_unique PRIMARY KEY (proofset, add_message_hash, subroot_offset),

    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE, -- cascade, if we drop a proofset, we no longer care about the roots
    FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE SET NULL -- sets null on delete so that it's easy to notice and clean up
);

-- proofset_refcount tracking
CREATE OR REPLACE FUNCTION increment_proofset_refcount()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET proofset_refcount = proofset_refcount + 1
    WHERE id = NEW.pdp_pieceref;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proofset_root_insert
    AFTER INSERT ON pdp_proofset_roots
    FOR EACH ROW
    WHEN (NEW.pdp_pieceref IS NOT NULL)
EXECUTE FUNCTION increment_proofset_refcount();

CREATE OR REPLACE FUNCTION decrement_proofset_refcount()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE pdp_piecerefs
    SET proofset_refcount = proofset_refcount - 1
    WHERE id = OLD.pdp_pieceref;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proofset_root_delete
    AFTER DELETE ON pdp_proofset_roots
    FOR EACH ROW
    WHEN (OLD.pdp_pieceref IS NOT NULL)
EXECUTE FUNCTION decrement_proofset_refcount();

CREATE OR REPLACE FUNCTION adjust_proofset_refcount_on_update()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.pdp_pieceref IS DISTINCT FROM NEW.pdp_pieceref THEN
        -- Decrement count for old reference if not null
        IF OLD.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET proofset_refcount = proofset_refcount - 1
            WHERE id = OLD.pdp_pieceref;
        END IF;
        -- Increment count for new reference if not null
        IF NEW.pdp_pieceref IS NOT NULL THEN
            UPDATE pdp_piecerefs
            SET proofset_refcount = proofset_refcount + 1
            WHERE id = NEW.pdp_pieceref;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proofset_root_update
    AFTER UPDATE ON pdp_proofset_roots
    FOR EACH ROW
EXECUTE FUNCTION adjust_proofset_refcount_on_update();

-- proofset creation request trigger
CREATE OR REPLACE FUNCTION update_pdp_proofset_creates()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        -- Update the ok field in pdp_proofset_creates if a matching entry exists
        UPDATE pdp_proofset_creates
        SET ok = CASE
                     WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                     WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                     ELSE ok
            END
        WHERE create_message_hash = NEW.signed_tx_hash AND proofset_created = FALSE;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proofset_create_message_status_change
    AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
    FOR EACH ROW
EXECUTE PROCEDURE update_pdp_proofset_creates();

-- add proofset add message trigger
CREATE OR REPLACE FUNCTION update_pdp_proofset_roots()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.tx_status = 'pending' AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed') THEN
        -- Update the add_message_ok field in pdp_proofset_root_adds if a matching entry exists
        UPDATE pdp_proofset_root_adds
        SET add_message_ok = CASE
                                WHEN NEW.tx_status = 'failed' OR NEW.tx_success = FALSE THEN FALSE
                                WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = TRUE THEN TRUE
                                ELSE add_message_ok
                            END
        WHERE add_message_hash = NEW.signed_tx_hash;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER pdp_proofset_add_message_status_change
    AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
    FOR EACH ROW
EXECUTE PROCEDURE update_pdp_proofset_roots();
