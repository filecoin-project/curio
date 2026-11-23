-- Filecoin Pay income of PDP data sets, recorded by PDPv0_Income. No table
-- references pdp_data_sets, so the records outlive their data set.

-- Rails of a data set as FWSS reports them.
CREATE TABLE IF NOT EXISTS pdp_data_set_rails (
    data_set BIGINT PRIMARY KEY,
    pdp_rail_id BIGINT NOT NULL,

    -- NULL for a data set without CDN.
    cache_miss_rail_id BIGINT,

    payer TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per Filecoin Pay payment event on a rail of a local data set.
-- Amounts are in the token's base units.
CREATE TABLE IF NOT EXISTS pdp_income_events (
    tx_hash TEXT NOT NULL,
    log_index BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    rail_id BIGINT NOT NULL,
    data_set BIGINT NOT NULL,

    -- 'settle' for RailSettled, 'one_time' for RailOneTimePaymentProcessed.
    kind TEXT NOT NULL,

    -- Amount received by the payee.
    net_amount NUMERIC(78, 0) NOT NULL,
    operator_commission NUMERIC(78, 0) NOT NULL,
    network_fee NUMERIC(78, 0) NOT NULL,

    -- Set for 'settle' only.
    total_amount NUMERIC(78, 0),
    settled_up_to BIGINT,

    PRIMARY KEY (tx_hash, log_index)
);

CREATE INDEX IF NOT EXISTS pdp_income_events_data_set_idx ON pdp_income_events (data_set);

-- Reading position of PDPv0_Income; next_height is NULL until the first run.
CREATE TABLE IF NOT EXISTS pdp_income_cursor (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    next_height BIGINT,
    started_height BIGINT,
    started_at TIMESTAMPTZ,

    -- Runs that failed to read next_height since the cursor reached it, and when the first failed.
    failed_runs INT NOT NULL DEFAULT 0,
    stalled_since TIMESTAMPTZ,

    -- Runs in a row that stopped before the finalized head.
    stopped_runs INT NOT NULL DEFAULT 0
);

INSERT INTO pdp_income_cursor (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- Skipped height ranges, inclusive; income paid in them is not recorded.
CREATE TABLE IF NOT EXISTS pdp_income_gaps (
    from_height BIGINT PRIMARY KEY,
    to_height BIGINT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
