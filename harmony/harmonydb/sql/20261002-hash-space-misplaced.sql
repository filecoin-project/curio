-- A disk with has_misplaced holds piece files outside its own ranges, such as
-- pieces migrated from piece-park. Lookups check it for every hash until
-- those files reach their owners.
ALTER TABLE hash_space_disk ADD COLUMN IF NOT EXISTS has_misplaced BOOLEAN NOT NULL DEFAULT FALSE;
