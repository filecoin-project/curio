-- Persistent alert history with acknowledgment support
-- Replaces the simple alerts table with a more comprehensive system

-- Create new alert_history table
CREATE TABLE IF NOT EXISTS alert_history (
    id SERIAL PRIMARY KEY,
    alert_name VARCHAR(255) NOT NULL,          -- Alert category (e.g., 'WindowPost', 'WinningPost', 'NowCheck')
    message TEXT NOT NULL,                      -- Alert message content
    machine_name VARCHAR(255),                  -- Machine that generated the alert (for NowCheck alerts)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    -- Acknowledgment fields
    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledged_by VARCHAR(255),
    acknowledged_at TIMESTAMP WITH TIME ZONE,
    
    -- Tracking
    sent_to_plugins BOOLEAN DEFAULT FALSE,      -- Whether alert was sent to external plugins
    sent_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_alert_history_created ON alert_history(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_unacked ON alert_history(acknowledged, created_at DESC) WHERE NOT acknowledged;
CREATE INDEX IF NOT EXISTS idx_alert_history_name ON alert_history(alert_name, created_at DESC);

-- Alert comments table
CREATE TABLE IF NOT EXISTS alert_comments (
    id SERIAL PRIMARY KEY,
    alert_id INTEGER NOT NULL REFERENCES alert_history(id) ON DELETE CASCADE,
    comment TEXT NOT NULL,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_comments_alert ON alert_comments(alert_id);

COMMENT ON TABLE alert_history IS 'Persistent storage of all alerts with acknowledgment tracking';
COMMENT ON TABLE alert_comments IS 'Comments added to alerts by operators';
