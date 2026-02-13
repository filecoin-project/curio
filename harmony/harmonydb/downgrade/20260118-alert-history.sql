-- Revert 20260118-alert-history.sql

DROP INDEX IF EXISTS idx_alert_comments_alert;
DROP TABLE IF EXISTS alert_comments;

DROP INDEX IF EXISTS idx_alert_history_name;
DROP INDEX IF EXISTS idx_alert_history_unacked;
DROP INDEX IF EXISTS idx_alert_history_created;
DROP TABLE IF EXISTS alert_history;
