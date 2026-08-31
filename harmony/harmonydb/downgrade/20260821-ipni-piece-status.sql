CREATE TABLE IF NOT EXISTS ipni_ad_fetches (
    ad_cid TEXT NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS ipni_ad_fetches_ad_cid_time ON ipni_ad_fetches(ad_cid, fetched_at DESC);
