-- Alert lifecycle model.
--
-- One-shot events are recorded in alert_history with kind='event'.
-- Ongoing conditions are tracked in alert_conditions while active. Resolution
-- moves the condition lifecycle details into alert_history with kind='condition'.

ALTER TABLE alert_history
    ADD COLUMN IF NOT EXISTS kind TEXT,
    ADD COLUMN IF NOT EXISTS system TEXT,
    ADD COLUMN IF NOT EXISTS subsystem TEXT,
    ADD COLUMN IF NOT EXISTS condition TEXT,
    ADD COLUMN IF NOT EXISTS condition_created_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS condition_last_seen_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS condition_resolved_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS condition_repeat_count BIGINT;

UPDATE alert_history
SET kind = 'event'
WHERE kind IS NULL;

ALTER TABLE alert_history
    ALTER COLUMN kind SET DEFAULT 'event',
    ALTER COLUMN kind SET NOT NULL,
    DROP CONSTRAINT IF EXISTS alert_history_transition_check,
    DROP COLUMN IF EXISTS transition;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'alert_history_kind_check'
    ) THEN
        ALTER TABLE alert_history
            ADD CONSTRAINT alert_history_kind_check
                CHECK (kind IN ('event', 'condition'));
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS alert_conditions (
    system TEXT NOT NULL,
    subsystem TEXT NOT NULL,
    condition TEXT NOT NULL,
    message TEXT NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    repeat_count BIGINT NOT NULL DEFAULT 0,
    last_notified_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (system, subsystem, condition)
);

ALTER TABLE alert_conditions
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS repeat_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_notified_at TIMESTAMP WITH TIME ZONE;

CREATE INDEX IF NOT EXISTS idx_alert_history_pending_events
    ON alert_history (created_at)
    WHERE kind = 'event' AND sent_at IS NULL;

COMMENT ON TABLE alert_conditions IS 'Current lifecycle state for active condition alerts';
COMMENT ON COLUMN alert_history.kind IS 'event for one-shot incidents, condition for lifecycle transitions';
