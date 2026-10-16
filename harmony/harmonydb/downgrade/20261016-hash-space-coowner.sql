ALTER TABLE hash_space_range DROP COLUMN IF EXISTS problem;

DELETE FROM hash_space_range a
 USING hash_space_range b
 WHERE a.space = b.space AND a.end_hash = b.end_hash AND a.storage_id > b.storage_id;

ALTER TABLE hash_space_range DROP CONSTRAINT IF EXISTS hash_space_range_pkey;
ALTER TABLE hash_space_range ADD PRIMARY KEY (space, end_hash);

ALTER TABLE hash_space_move_source DROP COLUMN IF EXISTS started_at;
