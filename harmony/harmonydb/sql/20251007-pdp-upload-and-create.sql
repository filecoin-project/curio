CREATE TABLE IF NOT EXISTS pdp_pending_piece_adds (
    create_message_hash TEXT PRIMARY KEY,
    service TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pdp_pending_piece_adds_service ON pdp_pending_piece_adds(service);


