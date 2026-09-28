-- Cluster view of the open-pieces / acl-pieces hash spaces.
--
-- hash_space_range is the single-owner tiling the solver works on. A range
-- covers (previous end_hash, end_hash] within its space.
-- hash_space_move_source is an interval being moved from from_storage to
-- to_storage; while the row exists, a piece in that interval may be found on
-- either disk.
-- open_piece records where each piece file is, one row per disk holding it.

CREATE TABLE IF NOT EXISTS hash_space_meta (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version BIGINT NOT NULL DEFAULT 0
);

INSERT INTO hash_space_meta (id, version) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS hash_space_disk (
    storage_id TEXT PRIMARY KEY,
    capacity BIGINT NOT NULL,
    used_open BIGINT NOT NULL DEFAULT 0,
    used_acl BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS hash_space_range (
    space TEXT NOT NULL,
    end_hash BYTEA NOT NULL,
    storage_id TEXT NOT NULL,
    PRIMARY KEY (space, end_hash)
);

CREATE INDEX IF NOT EXISTS idx_hash_space_range_storage ON hash_space_range (storage_id);

CREATE TABLE IF NOT EXISTS hash_space_move_source (
    id BIGSERIAL PRIMARY KEY,
    space TEXT NOT NULL,
    start_hash BYTEA NOT NULL,
    end_hash BYTEA NOT NULL,
    from_storage TEXT NOT NULL,
    to_storage TEXT NOT NULL,
    size BIGINT NOT NULL,
    task_id BIGINT NOT NULL, -- HashSpaceMove, added in the same transaction
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (space, start_hash, end_hash)
);

CREATE INDEX IF NOT EXISTS idx_hash_space_move_source_task ON hash_space_move_source (task_id);

-- At most one queued event per disk and kind while a rebalance is running.
CREATE TABLE IF NOT EXISTS hash_space_pending_event (
    storage_id TEXT NOT NULL,
    event_kind TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (storage_id, event_kind)
);

-- Hash spaces are keyed by piece CID v2 only.
CREATE TABLE IF NOT EXISTS open_piece (
    piece_cid TEXT NOT NULL, -- piece cid v2
    storage_id TEXT NOT NULL,
    space TEXT NOT NULL,
    piece_hash BYTEA NOT NULL,
    size BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (piece_cid, storage_id)
);

CREATE INDEX IF NOT EXISTS idx_open_piece_space_hash ON open_piece (space, piece_hash);
CREATE INDEX IF NOT EXISTS idx_open_piece_storage_hash ON open_piece (storage_id, space, piece_hash);

-- PDP pieces to move from piece-park into open-pieces, written with their
-- HashSpacePlace task in the transaction that creates the pdp_piecerefs row.
-- The v2 CID is derived from pdp_piece_cid and the parked piece's raw size.
CREATE TABLE IF NOT EXISTS hash_space_place (
    pdp_pieceref BIGINT PRIMARY KEY,
    pdp_piece_cid TEXT NOT NULL, -- pdp_piecerefs.piece_cid (v1)
    piece_ref BIGINT NOT NULL,
    task_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hash_space_place_task ON hash_space_place (task_id);

-- Pieces whose last PDP reference was dropped, written with their
-- HashSpaceDrop task; files are removed from every open_piece location.
-- started is set once the drop has checked no PDP ref came back; placement
-- of the same piece waits for a started drop.
CREATE TABLE IF NOT EXISTS hash_space_delete (
    piece_cid TEXT PRIMARY KEY, -- piece cid v2
    task_id BIGINT NOT NULL,
    started BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hash_space_delete_task ON hash_space_delete (task_id);
