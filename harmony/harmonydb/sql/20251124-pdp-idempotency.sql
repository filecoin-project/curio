-- Idempotency tracking for PDP operations
CREATE TABLE pdp_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    tx_hash TEXT, -- NULL initially, set after successful transaction
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Index for cleanup operations
CREATE INDEX pdp_idempotency_created_at_idx ON pdp_idempotency(created_at);
