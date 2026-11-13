-- Change detection polls harmony_config.timestamp. Writers historically
-- updated only `config`, so Dynamic fields never reloaded. Touch timestamp
-- on every config rewrite so EnableChangeDetection sees the change.
CREATE OR REPLACE FUNCTION harmony_config_touch_timestamp() RETURNS trigger AS $$
BEGIN
    NEW.timestamp := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS harmony_config_touch_timestamp ON harmony_config;
CREATE TRIGGER harmony_config_touch_timestamp
    BEFORE UPDATE OF config ON harmony_config
    FOR EACH ROW
    EXECUTE FUNCTION harmony_config_touch_timestamp();
