ALTER TABLE sectors_meta ADD COLUMN deadline BIGINT;
ALTER TABLE sectors_meta ADD COLUMN partition BIGINT;

-- index on deadline/partition/spid/sectornum
CREATE INDEX sectors_meta_deadline_partition_spid_sectornum_index ON sectors_meta(deadline, partition, sp_id, sector_num);

-- force sector metadata refresh
DELETE FROM harmony_task_singletons WHERE task_name = 'SectorMetadata' and task_id IS NULL;