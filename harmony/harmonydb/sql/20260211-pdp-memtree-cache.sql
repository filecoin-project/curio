-- PDP Merkle Tree Cache Schema
-- Single-table design consolidates cache data and task tracking
-- tree_data is NULL until status = 'completed'

CREATE TABLE IF NOT EXISTS pdp_piece_memtrees (
    id BIGSERIAL PRIMARY KEY,
    piece_cid TEXT NOT NULL,
    piece_padded_size BIGINT NOT NULL,
    
    -- Cache payload (NULL until completed)
    tree_data BYTEA,
    tree_size BIGINT,
    
    -- Task tracking
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, caching, completed, failed
    cache_task_id BIGINT,
    error_msg TEXT,
    
    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    
    UNIQUE(piece_cid, piece_padded_size)
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS pdp_memtree_status_idx ON pdp_piece_memtrees(status, created_at);
CREATE INDEX IF NOT EXISTS pdp_memtree_cache_task_idx ON pdp_piece_memtrees(cache_task_id);
CREATE INDEX IF NOT EXISTS pdp_memtree_cid_size_idx ON pdp_piece_memtrees(piece_cid, piece_padded_size);
