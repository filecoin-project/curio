-- Tracks table write churn observed at the time of the last ANALYZE issued by the
-- DBAnalyze task, so tables are only re-analyzed after meaningful growth.
CREATE TABLE IF NOT EXISTS table_analyze_state (
    table_name       TEXT PRIMARY KEY,
    churn_at_analyze BIGINT      NOT NULL,
    rows_at_analyze  BIGINT      NOT NULL DEFAULT 0,
    last_analyzed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    analyze_count    BIGINT      NOT NULL DEFAULT 0
);
