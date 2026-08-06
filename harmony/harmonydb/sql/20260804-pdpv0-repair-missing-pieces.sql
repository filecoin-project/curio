-- PDPv0 missing-piece repair (filecoin-project/curio#1359).
--
-- A piece could end up live on PDPVerifier with no matching row in
-- pdp_data_set_pieces. Proving then fails with "no subpiece found" for every
-- challenge that lands inside that piece, and the row's absence also leaves
-- pdp_piecerefs.data_set_refcount at 0, which makes the piece GC eligible.
--
-- The add-piece fix ships in this release, so only data sets that already
-- exist when this migration runs can carry the damage. The scrub queue is
-- therefore seeded once, here. Data sets created afterwards are never
-- enqueued and are never scrubbed.

CREATE TABLE IF NOT EXISTS pdp_data_set_piece_scrub (
    data_set BIGINT PRIMARY KEY REFERENCES pdp_data_sets(id) ON DELETE CASCADE,

    -- PDPVerifier piece id to resume getActivePiecesByCursor from, so a
    -- retried task does not re-walk pages it already reconciled.
    next_piece_id BIGINT NOT NULL DEFAULT 0,

    task_id BIGINT DEFAULT NULL,
    complete BOOLEAN NOT NULL DEFAULT FALSE,

    -- Bumped when a claimed task disappears without finishing. Bounds retries
    -- so a permanently failing data set cannot be requeued forever.
    failures BIGINT NOT NULL DEFAULT 0,

    pieces_checked BIGINT NOT NULL DEFAULT 0,
    pieces_repaired BIGINT NOT NULL DEFAULT 0,
    pieces_lost BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    completed_at TIMESTAMPTZ DEFAULT NULL
);

COMMENT ON TABLE pdp_data_set_piece_scrub IS
    'One-shot queue of data sets to reconcile against PDPVerifier active pieces; seeded at upgrade time only.';

-- The scheduler only ever looks for unclaimed, unfinished rows.
CREATE INDEX IF NOT EXISTS idx_pdp_data_set_piece_scrub_pending
    ON pdp_data_set_piece_scrub (data_set)
    WHERE complete = FALSE AND task_id IS NULL;

-- Reclaiming abandoned rows scans by task_id.
CREATE INDEX IF NOT EXISTS idx_pdp_data_set_piece_scrub_task_id
    ON pdp_data_set_piece_scrub (task_id)
    WHERE task_id IS NOT NULL;

INSERT INTO pdp_data_set_piece_scrub (data_set)
SELECT id FROM pdp_data_sets
ON CONFLICT (data_set) DO NOTHING;

-- Pieces PDPVerifier says we are storing but whose bytes we cannot find
-- locally. Repair cannot recover these, so they are recorded for an operator
-- to act on. Deliberately not tied to pdp_data_sets by a foreign key: the
-- record has to outlive deletion of the data set it came from.
CREATE TABLE IF NOT EXISTS lost_data (
    data_set BIGINT NOT NULL,
    piece_id BIGINT NOT NULL,

    -- PieceCID v2, exactly as held on chain. Kept even when it could not be
    -- decoded, in which case it is a 0x-prefixed hex dump of the raw bytes and
    -- the derived columns below are NULL.
    piece_cid TEXT NOT NULL,
    piece_cid_v1 TEXT DEFAULT NULL,
    piece_raw_size BIGINT DEFAULT NULL,

    reason TEXT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),

    PRIMARY KEY (data_set, piece_id)
);

COMMENT ON TABLE lost_data IS
    'Pieces live on PDPVerifier whose data could not be located in local storage during a repair scrub.';
