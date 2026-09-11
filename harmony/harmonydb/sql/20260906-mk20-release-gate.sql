-- A singleton write-conflict point for participating MK20 waiting releases.
-- This is a lock token only; it is not an active counter or reservation.
CREATE TABLE IF NOT EXISTS market_mk20_release_gate (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton = TRUE),
    token BOOLEAN NOT NULL DEFAULT FALSE
);

INSERT INTO market_mk20_release_gate (singleton, token)
VALUES (TRUE, FALSE)
ON CONFLICT (singleton) DO NOTHING;
