-- Revert 20260117-alert-mutes.sql

DROP INDEX IF EXISTS idx_alert_mutes_active;
DROP TABLE IF EXISTS alert_mutes;
