CREATE TABLE IF NOT EXISTS message_send_replacements (
    id bigserial PRIMARY KEY,

    from_key text NOT NULL,
    nonce bigint NOT NULL,

    -- The first Curio-sent message for this nonce.
    original_signed_cid text NOT NULL,

    -- The signed message this row attempts to replace.
    -- First replacement: replaces_signed_cid = original_signed_cid.
    -- Later replacement: replaces_signed_cid = previous successful replacement signed_cid.
    replaces_signed_cid text NOT NULL,

    claim_id text NOT NULL,
    claim_time timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    claim_until timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP + interval '5 minutes',

    signed_cid text,
    signed_data bytea,

    send_time timestamptz,
    send_success boolean,
    send_error text
);

CREATE INDEX IF NOT EXISTS message_send_replacements_latest_success_idx
    ON message_send_replacements (from_key, nonce, signed_cid)
    WHERE send_success = TRUE
      AND signed_cid IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS message_send_replacements_claim_idx
    ON message_send_replacements (from_key, nonce, replaces_signed_cid)
    WHERE send_success IS NOT FALSE;

CREATE INDEX IF NOT EXISTS message_send_replacements_signed_cid_idx
    ON message_send_replacements (signed_cid)
    WHERE signed_cid IS NOT NULL;


CREATE TABLE IF NOT EXISTS message_send_eth_replacements (
    id bigserial PRIMARY KEY,

    from_address text NOT NULL,
    nonce bigint NOT NULL,

    -- The first Curio-sent Eth transaction for this nonce.
    original_signed_hash text NOT NULL,

    -- The Eth transaction this row attempts to replace.
    -- First replacement: replaces_signed_hash = original_signed_hash.
    -- Later replacement: replaces_signed_hash = previous successful replacement signed_hash.
    replaces_signed_hash text NOT NULL,

    claim_id text NOT NULL,
    claim_time timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    claim_until timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP + interval '5 minutes',

    signed_hash text,
    signed_tx bytea,

    send_time timestamptz,
    send_success boolean,
    send_error text
);

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_latest_success_idx
    ON message_send_eth_replacements (from_address, nonce, signed_hash)
    WHERE send_success = TRUE
      AND signed_hash IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS message_send_eth_replacements_claim_idx
    ON message_send_eth_replacements (from_address, nonce, replaces_signed_hash)
    WHERE send_success IS NOT FALSE;

CREATE INDEX IF NOT EXISTS message_send_eth_replacements_signed_hash_idx
    ON message_send_eth_replacements (signed_hash)
    WHERE signed_hash IS NOT NULL;