-- Persist extraData for CreateDataSet / create-and-add so lost-receipt recovery
-- can resolve payer + clientDataSetId without relying solely on signed tx calldata.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_creates'
          AND column_name = 'extra_data'
    ) THEN
        ALTER TABLE pdp_data_set_creates
            ADD COLUMN extra_data BYTEA;
    END IF;
END $$;
