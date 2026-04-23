-- Track IPNI advertisement fetches to provide indexing status visibility
-- This table logs when advertisements are fetched by indexers

CREATE TABLE IF NOT EXISTS ipni_ad_fetches (
    ad_cid TEXT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for efficient lookup by ad_cid and time-based queries
CREATE INDEX IF NOT EXISTS ipni_ad_fetches_ad_cid_time ON ipni_ad_fetches(ad_cid, fetched_at DESC);
