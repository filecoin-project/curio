-- PDPv0 chain reorg audit: track rollbacks and hard-loss cases.

CREATE TABLE IF NOT EXISTS pdpv0_reorg_events (
    id BIGSERIAL PRIMARY KEY,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tx_hash TEXT NOT NULL,
    send_reason TEXT NOT NULL,
    rollback_summary TEXT NOT NULL,
    data_set_id BIGINT,
    UNIQUE (tx_hash)
);

CREATE TABLE IF NOT EXISTS pdpv0_reorg_orphans (
    tx_hash TEXT PRIMARY KEY,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    data_set_id BIGINT,
    notes TEXT NOT NULL
);

COMMENT ON TABLE pdpv0_reorg_events IS 'PDPv0 reorg checker: one row per reorged ETH tx that was rolled back.';
COMMENT ON TABLE pdpv0_reorg_orphans IS 'PDPv0 reorg checker: unrecoverable local state (e.g. deleteDataSet already applied).';
