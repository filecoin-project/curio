-- Indexer-confirmed processing time per ad (storetheindex IndexedTime/SkippedTime,
-- ipni/storetheindex#2898), filled in by a background poller, not the announce path.
ALTER TABLE ipni ADD COLUMN IF NOT EXISTS indexed_at TIMESTAMPTZ DEFAULT NULL;

-- Existing rows predate this tracking and can never be confirmed retroactively,
-- so mark them with a sentinel (must match ipni_provider.UnconfirmedSyncSentinel)
-- rather than NULL, keeping them out of the poller query below. Explicit UTC
-- offset: a bare 'timestamptz' literal parses in the session's default timezone.
UPDATE ipni SET indexed_at = TIMESTAMPTZ '1970-01-01T00:00:00Z' WHERE indexed_at IS NULL;

-- Partial so it only holds ads still awaiting confirmation.
CREATE INDEX IF NOT EXISTS idx_ipni_pending_sync ON ipni (order_number)
    WHERE is_rm = FALSE AND indexed_at IS NULL;
