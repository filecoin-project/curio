-- Optional reference from an alert to the object it is about, e.g. ('dataset', '42').
ALTER TABLE alert_history ADD COLUMN IF NOT EXISTS subject_kind TEXT;
ALTER TABLE alert_history ADD COLUMN IF NOT EXISTS subject_id TEXT;
