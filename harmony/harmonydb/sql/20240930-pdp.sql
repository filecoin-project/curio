CREATE TABLE pdp_owner_addresses (
    owner_address TEXT NOT NULL PRIMARY KEY,
    private_key BYTEA NOT NULL
);

-- PDP services authenticate with ecdsa-sha256 keys; Allowed services here
CREATE TABLE pdp_services (
    id BIGSERIAL PRIMARY KEY,
    pubkey BYTEA NOT NULL,

    -- service_url TEXT NOT NULL,
    service_label TEXT NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(pubkey)
);

-- PDP piece references, this table tells Curio which pieces in storage are managed by PDP
CREATE TABLE pdp_piecerefs (
    id BIGSERIAL PRIMARY KEY,
    service_id BIGINT NOT NULL, -- pdp_services.id
    piece_cid TEXT NOT NULL, -- piece cid v2
    ref_id TEXT NOT NULL, -- parked_piece_refs.ref_id
    service_tag VARCHAR(64), -- service tag, pulled from the JWT
    client_tag VARCHAR(64), -- client tag, client-specified
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(ref_id),
    FOREIGN KEY (service_id) REFERENCES pdp_services(id) ON DELETE CASCADE,
    FOREIGN KEY (ref_id) REFERENCES parked_piece_refs(ref_id) ON DELETE CASCADE
);

-- PDP proofsets we maintain
CREATE TABLE pdp_proof_sets (
    id BIGINT PRIMARY KEY, -- on-chain proofset id

    -- cached chain values
    next_challenge_epoch BIGINT -- next challenge epoch
);

-- proofset roots
CREATE TABLE pdp_proofset_roots (
    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
    root_id BIGINT NOT NULL, -- on-chain index of the root in the rootCids sub-array
    root TEXT NOT NULL, -- root cid (piececid v2)

    -- aggregation roots (aggregated like pieces in filecoin sectors)
    subroot TEXT NOT NULL, -- subroot cid (piececid v2), with no aggregation this == root
    subroot_offset BIGINT NOT NULL, -- offset of the subroot in the root
    -- note: size contained in subroot piececid v2

    pdp_pieceref BIGINT NOT NULL, -- pdp_piecerefs.id

    CONSTRAINT pdp_proofset_roots_pk PRIMARY KEY (proofset, root_id, subroot_offset),

    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE, -- cascade, if we drop a proofset, we no longer care about the roots
    FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE SET NULL -- sets null on delete so that it's easy to notice and clean up
);

CREATE TABLE pdp_prove_tasks (
    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
    challenge_epoch BIGINT NOT NULL, -- challenge epoch

    task_id BIGINT NOT NULL, -- harmonytask task ID

    message_cid          text,
    message_eth_hash     text,

    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE,
    CONSTRAINT pdp_prove_tasks_pk PRIMARY KEY (proofset, challenge_epoch)
)
