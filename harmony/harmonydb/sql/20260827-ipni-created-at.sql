-- Used for ordering (pdp/handlers.go) and latency (market/ipni/ipni-provider).
ALTER TABLE ipni ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW());
