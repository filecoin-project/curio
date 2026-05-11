DROP TABLE IF EXISTS market_fix_raw_size;

CREATE TABLE IF NOT EXISTS market_fix_raw_size (
    id TEXT PRIMARY KEY,
    task_id BIGINT NOT NULL
);