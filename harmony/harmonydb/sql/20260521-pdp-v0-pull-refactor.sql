-- PDPv0 pull pipeline refactor.
--
-- Pull items are now scheduled by unique piece key and written directly to
-- long-term piece storage. Pull item terminal state is explicit so status no
-- longer has to be inferred from StorePiece/parked_pieces task state.

ALTER TABLE pdp_piece_pulls
    ADD COLUMN IF NOT EXISTS client_address TEXT NOT NULL DEFAULT '';

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS complete BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ;

UPDATE pdp_piece_pull_items fi
SET created_at = COALESCE(pp.created_at, NOW())
FROM pdp_piece_pulls pp
WHERE pp.id = fi.fetch_id
    AND fi.created_at IS NULL;

UPDATE pdp_piece_pull_items
SET created_at = NOW()
WHERE created_at IS NULL;

ALTER TABLE pdp_piece_pull_items
    ALTER COLUMN created_at SET DEFAULT NOW();

ALTER TABLE pdp_piece_pull_items
    ALTER COLUMN created_at SET NOT NULL;

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS parked_piece_ref BIGINT REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL;

ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS pull_parked_piece_id BIGINT REFERENCES parked_pieces(id) ON DELETE SET NULL;

ALTER TABLE pdp_piece_pull_items
    DROP CONSTRAINT IF EXISTS pdp_piece_pull_items_pkey;

ALTER TABLE pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_pkey PRIMARY KEY (fetch_id, piece_cid, source_url);

UPDATE pdp_piece_pull_items fi
SET task_id = NULL
WHERE fi.task_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM harmony_task ht
        WHERE ht.id = fi.task_id
    );

ALTER TABLE pdp_piece_pull_items
    DROP CONSTRAINT IF EXISTS pdp_piece_pull_items_terminal_state_check;

ALTER TABLE pdp_piece_pull_items
    ADD CONSTRAINT pdp_piece_pull_items_terminal_state_check
    CHECK (NOT (complete AND failed));
