CREATE TABLE storage_removal_marks (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,
    sector_filetype TEXT NOT NULL,
    storage_id TEXT NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,

    approved BOOLEAN NOT NULL DEFAULT FALSE,
    approved_at TIMESTAMP WITH TIME ZONE,

    sweep_task_id BIGINT,

    primary key (sp_id, sector_num, sector_filetype, storage_id)
);

CREATE TABLE storage_gc_pins (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,

    primary key (sp_id, sector_num)
);
