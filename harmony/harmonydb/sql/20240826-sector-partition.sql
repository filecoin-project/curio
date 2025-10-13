ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS deadline BIGINT;
ALTER TABLE sectors_meta ADD COLUMN IF NOT EXISTS partition BIGINT;

-- index on deadline/partition/spid/sectornum
CREATE INDEX IF NOT EXISTS sectors_meta_deadline_partition_spid_sectornum_index ON sectors_meta(deadline, partition, sp_id, sector_num);

-- schedule delay in case the sector got into an immutable deadline
ALTER TABLE sectors_snap_pipeline ADD COLUMN IF NOT EXISTS submit_after TIMESTAMP WITH TIME ZONE;

-- force sector metadata refresh
DELETE FROM harmony_task_singletons WHERE task_name = 'SectorMetadata' and task_id IS NULL;