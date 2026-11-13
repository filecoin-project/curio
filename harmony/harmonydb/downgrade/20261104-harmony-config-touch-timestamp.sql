-- Drop the config-touch trigger so older nodes keep the pre-20261104 write behavior.
DROP TRIGGER IF EXISTS harmony_config_touch_timestamp ON harmony_config;
DROP FUNCTION IF EXISTS harmony_config_touch_timestamp();
