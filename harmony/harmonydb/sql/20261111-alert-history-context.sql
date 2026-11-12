-- Optional references from an alert to the objects it is about, e.g. {"dataset": "42", "rail": "7"}.
ALTER TABLE alert_history ADD COLUMN IF NOT EXISTS context JSONB;
