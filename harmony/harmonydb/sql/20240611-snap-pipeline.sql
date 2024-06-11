CREATE TABLE sectors_snap_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,

    start_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    upgrade_proof INT NOT NULL,

    -- preload
    -- todo sector preload logic
    data_assigned BOOLEAN NOT NULL DEFAULT FALSE,

    -- encode
    update_unsealed_cid TEXT,
    update_sealed_cid TEXT,

    task_id_encode BIGINT,
    after_encode BOOLEAN NOT NULL DEFAULT FALSE,

    -- prove
    proof BYTEA,

    task_id_prove BIGINT,
    after_prove BOOLEAN NOT NULL DEFAULT FALSE,

    -- submit
    task_id_submit BIGINT,
    after_submit BOOLEAN NOT NULL DEFAULT FALSE,

    -- move storage
    task_id_move_storage BIGINT,
    after_move_storage BOOLEAN NOT NULL DEFAULT FALSE,

    FOREIGN KEY (sp_id, sector_number) REFERENCES sectors_meta (sp_id, sector_num),
    PRIMARY KEY (sp_id, sector_number)
);

create table sectors_snap_initial_pieces (
    sp_id bigint not null,
    sector_number bigint not null,

    piece_index bigint not null,
    piece_cid text not null,
    piece_size bigint not null, -- padded size

    -- data source
    data_url text not null,
    data_headers jsonb not null default '{}',
    data_raw_size bigint not null,
    data_delete_on_finalize bool not null,

    -- deal info
    f05_publish_cid text,
    f05_deal_id bigint,
    f05_deal_proposal jsonb,
    f05_deal_start_epoch bigint,
    f05_deal_end_epoch bigint,

    direct_start_epoch bigint,
    direct_end_epoch bigint,
    direct_piece_activation_manifest jsonb,

    -- foreign key
    foreign key (sp_id, sector_number) references sectors_snap_pipeline (sp_id, sector_number) on delete cascade,

    primary key (sp_id, sector_number, piece_index)
);
