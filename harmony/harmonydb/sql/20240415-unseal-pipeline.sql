create table sectors_unseal_pipeline (
    sp_id bigint not null,
    sector_number bigint not null,
    reg_seal_proof bigint not null,

    create_time timestamp not null default current_timestamp,

    task_id_unseal_sdr bigint, -- builds unseal cache
    after_unseal_sdr bool not null default false,

    task_id_decode_sector bigint, -- makes the "unsealed" copy (runs in target long-term storage)
    after_decode_sector bool not null default false,

    primary key (sp_id, sector_number)
);