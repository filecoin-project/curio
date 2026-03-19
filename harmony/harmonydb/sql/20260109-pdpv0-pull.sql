-- PDP piece pull tables for SP-to-SP transfer
--
-- Provides idempotency and piece tracking for pull requests.
-- Status is derived dynamically from parked_pieces, not stored here.
CREATE TABLE pdp_piece_pulls (
    id BIGSERIAL PRIMARY KEY,
    service TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    extra_data_hash BYTEA NOT NULL,  -- sha256(extraData) for idempotency
    data_set_id BIGINT NOT NULL DEFAULT 0,  -- 0 = create new dataset
    record_keeper TEXT NOT NULL DEFAULT '',  -- required when data_set_id is 0
    created_at TIMESTAMPTZ DEFAULT NOW(),

    UNIQUE(service, extra_data_hash, data_set_id, record_keeper)
);

-- Tracks individual pieces within a pull request
CREATE TABLE pdp_piece_pull_items (
    fetch_id BIGINT NOT NULL REFERENCES pdp_piece_pulls(id) ON DELETE CASCADE,
    piece_cid TEXT NOT NULL,        -- PieceCIDv1 (for joins with parked_pieces)
    piece_raw_size BIGINT NOT NULL, -- raw size to reconstruct PieceCIDv2 for API
    source_url TEXT NOT NULL,       -- external SP URL to fetch from
    task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,  -- pull task
    failed BOOLEAN NOT NULL DEFAULT FALSE,  -- true if piece permanently failed
    fail_reason TEXT,               -- error message when failed

    PRIMARY KEY (fetch_id, piece_cid)
);

-- Index for cleanup queries
CREATE INDEX idx_pdp_piece_pulls_created_at ON pdp_piece_pulls(created_at);
