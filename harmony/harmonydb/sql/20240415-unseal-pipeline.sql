create table sectors_unseal_pipeline (
    sp_id bigint not null,
    sector_number bigint not null,

    create_time timestamp not null default current_timestamp,

    task_id_unseal_sdr bigint, -- builds unseal cache
    after_unseal_sdr bool not null default false,

    task_id_decode_sector bigint, -- makes the "unsealed" copy (runs either next to unseal cache OR in long-term storage)
    after_decode_sector bool not null default false,

    task_id_move_storage bigint, -- optional, moves the unsealed sector to storage
    after_move_storage bool not null default false,

    primary key (sp_id, sector_number)
);