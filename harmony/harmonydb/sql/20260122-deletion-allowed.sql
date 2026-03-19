-- Add deletion_allowed column to gate dataset deletion on settlement finalization
-- Deletion should only proceed when the rail is fully settled (endEpoch == settledUpTo)
ALTER TABLE pdp_delete_data_set ADD COLUMN deletion_allowed BOOLEAN NOT NULL DEFAULT FALSE;
