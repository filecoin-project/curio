CREATE TABLE IF NOT EXISTS harmony_config_history (
    id SERIAL PRIMARY KEY,
    title VARCHAR(300) NOT NULL,
    config TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_config_history_title_time ON harmony_config_history(title, changed_at DESC);
