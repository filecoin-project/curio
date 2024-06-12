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

    created_at timestamp with time zone not null,

    piece_index bigint not null,
    piece_cid text not null,
    piece_size bigint not null, -- padded size

    -- data source
    data_url text not null,
    data_headers jsonb not null default '{}',
    data_raw_size bigint not null,
    data_delete_on_finalize bool not null,

    -- deal info
    direct_start_epoch bigint,
    direct_end_epoch bigint,
    direct_piece_activation_manifest jsonb,

    -- foreign key
    foreign key (sp_id, sector_number) references sectors_snap_pipeline (sp_id, sector_number) on delete cascade,

    primary key (sp_id, sector_number, piece_index)
);

CREATE OR REPLACE FUNCTION transfer_and_delete_open_piece_snap(v_sp_id bigint, v_sector_number bigint)
    RETURNS void AS $$
BEGIN
    -- if the related open_sector_pieces.f05_deal_id is not null, raise an exception
    IF EXISTS (
        SELECT 1
        FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number AND f05_deal_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Cannot transfer open_sector_pieces with f05_deal_id not null for sp_id % and sector_number %', v_sp_id, v_sector_number;
    END IF;

    -- Copy data from open_sector_pieces to sectors_sdr_initial_pieces
    INSERT INTO sectors_snap_initial_pieces (
        sp_id,
        sector_number,
        piece_index,
        piece_cid,
        piece_size,
        data_url,
        data_headers,
        data_raw_size,
        data_delete_on_finalize,
        direct_start_epoch,
        direct_end_epoch,
        direct_piece_activation_manifest,
        created_at
    )
    SELECT
        sp_id,
        sector_number,
        piece_index,
        piece_cid,
        piece_size,
        data_url,
        data_headers,
        data_raw_size,
        data_delete_on_finalize,
        direct_start_epoch,
        direct_end_epoch,
        direct_piece_activation_manifest,
        created_at
    FROM
        open_sector_pieces
    WHERE
        sp_id = v_sp_id AND
        sector_number = v_sector_number;

-- Check for successful insertion, then delete the corresponding row from open_sector_pieces
    IF FOUND THEN
        DELETE FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number;
    ELSE
        RAISE EXCEPTION 'No data found to transfer for sp_id % and sector_number %', v_sp_id, v_sector_number;
    END IF;
END;
$$ LANGUAGE plpgsql;

