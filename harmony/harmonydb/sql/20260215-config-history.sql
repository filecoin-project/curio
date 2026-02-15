CREATE TABLE IF NOT EXISTS harmony_config_history (
    id SERIAL PRIMARY KEY,
    title VARCHAR(300) NOT NULL,
    old_config TEXT NOT NULL,
    new_config TEXT NOT NULL,
    changed_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_config_history_title ON harmony_config_history(title);
CREATE INDEX IF NOT EXISTS idx_config_history_changed_at ON harmony_config_history(changed_at DESC);
