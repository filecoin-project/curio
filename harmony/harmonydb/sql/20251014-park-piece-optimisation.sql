CREATE INDEX IF NOT EXISTS idx_parked_piece_refs_piece_id
    ON parked_piece_refs (piece_id);

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_null
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL;