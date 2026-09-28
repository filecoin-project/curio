-- A disk whose layout.json asks to vacate must not receive new ranges.
ALTER TABLE hash_space_disk ADD COLUMN IF NOT EXISTS vacating BOOLEAN NOT NULL DEFAULT FALSE;
