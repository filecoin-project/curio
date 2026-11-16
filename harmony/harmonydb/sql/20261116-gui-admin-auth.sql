-- Admin GUI username/password with JWT session signing material (cluster-wide).
-- Sessions are signed cookies (key+salt); no per-session rows.
CREATE TABLE IF NOT EXISTS gui_admin (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    session_key BYTEA NOT NULL,
    session_salt BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_password_attempt_at TIMESTAMPTZ
);
