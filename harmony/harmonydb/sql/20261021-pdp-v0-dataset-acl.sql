-- Cache foc/acl intent on the data set so piece placement does not depend on
-- a live chain metadata read. NULL means unresolved.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND column_name = 'acl'
    ) THEN
        ALTER TABLE pdp_data_sets
            ADD COLUMN acl BOOLEAN;
    END IF;
END $$;

COMMENT ON COLUMN pdp_data_sets.acl IS
    'foc/acl intent: TRUE when metadata key foc/acl is "true", FALSE once known otherwise, NULL until resolved from create extra_data or chain';
