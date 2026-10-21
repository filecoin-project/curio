-- Cluster view of the open-pieces / acl-pieces hash spaces.
-- space is the folder name of one of those hash spaces: "open-pieces" or "acl-pieces".
--
-- hash_space_range tiles each space. A range covers (previous end_hash, end_hash]
-- within its space. size is the range's bytes as published by the node that
-- holds the disk. A stuck move leaves that range owned by both disks: the
-- second row shares end_hash and is the other owner. problem marks the owner
-- that failed, so new files in the range are written to the other disk.
-- hash_space_move_source is an interval being moved from from_storage to
-- to_storage; while the row exists, a piece in that interval may be found on
-- either disk. started_at records when HashSpaceMove first began the row, so
-- a row that never runs can be told from one that is retrying.

CREATE TABLE IF NOT EXISTS hash_space_meta (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version BIGINT NOT NULL DEFAULT 0
);

INSERT INTO hash_space_meta (id, version) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;

-- vacating: layout.json asked to vacate; the disk must not receive new ranges.
-- has_misplaced: the disk holds piece files outside its own ranges, such as
-- pieces migrated from piece-park. Lookups check it for every hash until
-- those files reach their owners.
CREATE TABLE IF NOT EXISTS hash_space_disk (
    storage_id TEXT PRIMARY KEY,
    capacity BIGINT NOT NULL,
    used_open BIGINT NOT NULL DEFAULT 0,
    used_acl BIGINT NOT NULL DEFAULT 0,
    vacating BOOLEAN NOT NULL DEFAULT FALSE,
    has_misplaced BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS hash_space_range (
    space TEXT NOT NULL, -- "open-pieces" or "acl-pieces"
    end_hash BYTEA NOT NULL,
    storage_id TEXT NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    problem BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (space, end_hash, storage_id)
);

CREATE INDEX IF NOT EXISTS idx_hash_space_range_storage ON hash_space_range (storage_id);

CREATE TABLE IF NOT EXISTS hash_space_move_source (
    id BIGSERIAL PRIMARY KEY,
    space TEXT NOT NULL, -- "open-pieces" or "acl-pieces"
    start_hash BYTEA NOT NULL,
    end_hash BYTEA NOT NULL,
    from_storage TEXT NOT NULL,
    to_storage TEXT NOT NULL,
    size BIGINT NOT NULL,
    task_id BIGINT NOT NULL, -- HashSpaceMove, added in the same transaction
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
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
-- HashSpaceDrop task. The piece file is removed from the disks that hold
-- its hash: the range owner, or both ends of a move that covers it.
-- started is set once the drop has checked no PDP ref came back; placement
-- of the same piece waits for a started drop.
CREATE TABLE IF NOT EXISTS hash_space_delete (
    piece_cid TEXT PRIMARY KEY, -- piece cid v2
    task_id BIGINT NOT NULL,
    started BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hash_space_delete_task ON hash_space_delete (task_id);
