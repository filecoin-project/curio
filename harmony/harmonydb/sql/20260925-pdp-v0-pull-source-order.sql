-- Client try order for pull source URLs. Lower values are attempted first.
ALTER TABLE pdp_piece_pull_items
    ADD COLUMN IF NOT EXISTS source_ord INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN pdp_piece_pull_items.source_ord IS
    'Order in which ingest should try this URL for the piece. Lower values are first.';
