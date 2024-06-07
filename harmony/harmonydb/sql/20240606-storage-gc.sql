CREATE TABLE storage_removal_marks (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,
    sector_filetype TEXT NOT NULL,
    storage_id TEXT NOT NULL,

    was_onchain BOOLEAN NOT NULL,
    was_seal_failed BOOLEAN NOT NULL,
    was_owned_sp_id BOOLEAN NOT NULL,

    primary key (sp_id, sector_num, sector_filetype, storage_id)
);

CREATE TABLE storage_gc_pins (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,
    sector_filetype TEXT, -- null = all file types
    storage_id TEXT, -- null = all storage ids

    primary key (sp_id, sector_num, sector_filetype, storage_id)
);
