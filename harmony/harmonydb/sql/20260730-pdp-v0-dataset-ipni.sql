-- Cache withIPFSIndexing intent on the data set so AddPieces does not depend on
-- live chain metadata reads. NULL means unresolved (backfill / first resolve).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND column_name = 'ipni'
    ) THEN
        ALTER TABLE pdp_data_sets
            ADD COLUMN ipni BOOLEAN;
    END IF;
END $$;

COMMENT ON COLUMN pdp_data_sets.ipni IS
    'withIPFSIndexing intent: TRUE/FALSE once known, NULL until resolved from create extra_data or chain';
