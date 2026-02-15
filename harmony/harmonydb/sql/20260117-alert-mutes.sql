-- Alert muting mechanism
-- Allows operators to mute specific alerts by pattern

CREATE TABLE IF NOT EXISTS alert_mutes (
    id SERIAL PRIMARY KEY,
    alert_name VARCHAR(255) NOT NULL,           -- Name of the alert category (e.g., 'WindowPost', 'WinningPost', 'NowCheck')
    pattern TEXT,                                -- Optional pattern to match within alert message (SQL LIKE pattern)
    reason TEXT NOT NULL,                        -- Reason for muting
    muted_by VARCHAR(255) NOT NULL,              -- Who muted this
    muted_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE,         -- NULL means never expires
    active BOOLEAN DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_alert_mutes_active ON alert_mutes(active, alert_name);

COMMENT ON TABLE alert_mutes IS 'Stores muted alert patterns to suppress specific alerts';
COMMENT ON COLUMN alert_mutes.pattern IS 'SQL LIKE pattern to match alert messages. NULL means mute entire alert category.';
