-- A stuck move leaves that range owned by both disks. The second row shares
-- end_hash; it is the other owner of the same range. problem marks the owner
-- that failed, so new files in the range are written to the other disk.
-- started_at records when HashSpaceMove first began the row, so a row that
-- never runs can be told from one that is retrying.
ALTER TABLE hash_space_range DROP CONSTRAINT IF EXISTS hash_space_range_pkey;
ALTER TABLE hash_space_range ADD PRIMARY KEY (space, end_hash, storage_id);
ALTER TABLE hash_space_range ADD COLUMN IF NOT EXISTS problem BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE hash_space_move_source ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
