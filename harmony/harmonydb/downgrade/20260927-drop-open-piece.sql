ALTER TABLE hash_space_range DROP COLUMN IF EXISTS size;

CREATE TABLE IF NOT EXISTS open_piece (
    piece_cid TEXT NOT NULL,
    storage_id TEXT NOT NULL,
    space TEXT NOT NULL,
    piece_hash BYTEA NOT NULL,
    size BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (piece_cid, storage_id)
);
