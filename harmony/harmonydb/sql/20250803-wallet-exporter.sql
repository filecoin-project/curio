CREATE TABLE IF NOT EXISTS wallet_exporter_processing (
    singleton BOOLEAN NOT NULL DEFAULT TRUE PRIMARY KEY CHECK (singleton = TRUE),
    processed_until TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

DELETE FROM wallet_exporter_processing WHERE singleton = TRUE;
INSERT INTO wallet_exporter_processing (singleton) VALUES (TRUE);

-- presence of a message in this table means that we've already accounted the basic send
CREATE TABLE IF NOT EXISTS wallet_exporter_watched_msgs (
    msg_cid TEXT PRIMARY KEY REFERENCES message_waits(signed_message_cid) ON DELETE CASCADE,

    observed_landed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW() -- used for gc
);

CREATE INDEX IF NOT EXISTS wallet_exporter_watched_msgs_observed_landed ON wallet_exporter_watched_msgs (created_at ASC);
CREATE INDEX IF NOT EXISTS wallet_exporter_watched_msgs_observed_landed_idx ON wallet_exporter_watched_msgs (observed_landed);
