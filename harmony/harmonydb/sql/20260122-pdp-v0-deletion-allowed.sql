-- Add deletion_allowed column to gate dataset deletion on settlement finalization
-- Deletion should only proceed when the rail is fully settled (endEpoch == settledUpTo)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'deletion_allowed'
    ) THEN
        ALTER TABLE pdp_delete_data_set ADD COLUMN IF NOT EXISTS deletion_allowed BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END
$$;
