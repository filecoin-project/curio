ALTER TABLE pdp_piece_pulls
    ADD COLUMN IF NOT EXISTS client_address TEXT NOT NULL DEFAULT '';

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS complete BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS parked_piece_ref BIGINT REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL;

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS pull_parked_piece_id BIGINT REFERENCES parked_pieces(id) ON DELETE SET NULL;

ALTER TABLE pdp_piece_pull_items
    DROP CONSTRAINT IF EXISTS pdp_piece_pull_items_pkey;

ALTER TABLE pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_pkey PRIMARY KEY (fetch_id, piece_cid, source_url);

ALTER TABLE pdp_piece_pull_items
    DROP CONSTRAINT IF EXISTS pdp_piece_pull_items_terminal_state_check;

ALTER TABLE pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_terminal_state_check
    CHECK (NOT (complete AND failed));

-- Support ON DELETE SET NULL from parked_piece_refs and parked_pieces without
-- scanning all pull items for each deleted parent row.
CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_parked_piece_ref
    ON pdp_piece_pull_items (parked_piece_ref);

CREATE INDEX IF NOT EXISTS idx_pdp_piece_pull_items_pull_parked_piece_id
    ON pdp_piece_pull_items (pull_parked_piece_id);

UPDATE pdp_piece_pull_items fi
SET created_at = COALESCE(pp.created_at, fi.created_at)
    FROM pdp_piece_pulls pp
WHERE pp.id = fi.fetch_id;

UPDATE pdp_piece_pull_items fi
SET task_id = NULL
WHERE fi.task_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM harmony_task ht
    WHERE ht.id = fi.task_id
);
