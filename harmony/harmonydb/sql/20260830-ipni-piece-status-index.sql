CREATE INDEX IF NOT EXISTS ipni_piece_cid_created_at
    ON ipni (piece_cid HASH, created_at DESC, order_number DESC);
