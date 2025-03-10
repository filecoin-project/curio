CREATE OR REPLACE FUNCTION transfer_and_delete_sorted_open_piece(v_sp_id bigint, v_sector_number bigint)
RETURNS void AS $$
DECLARE
sorted_pieces RECORD;
new_index INT := 0;
BEGIN
    -- Sort open_sector_pieces by piece_size in descending order and update piece_index
    FOR sorted_pieces IN
    SELECT piece_index AS old_index, piece_size, piece_cid, data_url, data_headers,
           data_raw_size, data_delete_on_finalize, f05_publish_cid, f05_deal_id,
           f05_deal_proposal, f05_deal_start_epoch, f05_deal_end_epoch, direct_start_epoch,
           direct_end_epoch, direct_piece_activation_manifest, created_at
    FROM open_sector_pieces
    WHERE sp_id = v_sp_id AND sector_number = v_sector_number
    ORDER BY piece_size DESC  -- Descending order for biggest size first

    LOOP
        -- Insert sorted data into sectors_sdr_initial_pieces with updated piece_index
        INSERT INTO sectors_sdr_initial_pieces (
            sp_id,
            sector_number,
            piece_index,
            piece_cid,
            piece_size,
            data_url,
            data_headers,
            data_raw_size,
            data_delete_on_finalize,
            f05_publish_cid,
            f05_deal_id,
            f05_deal_proposal,
            f05_deal_start_epoch,
            f05_deal_end_epoch,
            direct_start_epoch,
            direct_end_epoch,
            direct_piece_activation_manifest,
            created_at
        )
        VALUES (
                   v_sp_id,
                   v_sector_number,
                   new_index,  -- Insert new_index as piece_index
                   sorted_pieces.piece_cid,
                   sorted_pieces.piece_size,
                   sorted_pieces.data_url,
                   sorted_pieces.data_headers,
                   sorted_pieces.data_raw_size,
                   sorted_pieces.data_delete_on_finalize,
                   sorted_pieces.f05_publish_cid,
                   sorted_pieces.f05_deal_id,
                   sorted_pieces.f05_deal_proposal,
                   sorted_pieces.f05_deal_start_epoch,
                   sorted_pieces.f05_deal_end_epoch,
                   sorted_pieces.direct_start_epoch,
                   sorted_pieces.direct_end_epoch,
                   sorted_pieces.direct_piece_activation_manifest,
                   sorted_pieces.created_at
               );

        new_index := new_index + 1;
    END LOOP;


    -- Delete entries from open_sector_pieces after successful transfer
    IF FOUND THEN
        DELETE FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number;
    ELSE
        RAISE EXCEPTION 'No data found to transfer for sp_id % and sector_number %', v_sp_id, v_sector_number;
    END IF;

END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION transfer_and_delete_sorted_open_piece_snap(v_sp_id bigint, v_sector_number bigint)
RETURNS void AS $$
DECLARE
sorted_pieces RECORD;
new_index INT := 0;
BEGIN
    -- If the related open_sector_pieces.f05_deal_id is not null, raise an exception
    IF EXISTS (
        SELECT 1
        FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number AND f05_deal_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Cannot transfer open_sector_pieces with f05_deal_id not null for sp_id % and sector_number %', v_sp_id, v_sector_number;
    END IF;

    -- Sort open_sector_pieces by piece_size in descending order and update piece_index
    FOR sorted_pieces IN
    SELECT piece_index AS old_index, piece_size, piece_cid, data_url, data_headers,
           data_raw_size, data_delete_on_finalize, direct_start_epoch,
           direct_end_epoch, direct_piece_activation_manifest, created_at
    FROM open_sector_pieces
    WHERE sp_id = v_sp_id AND sector_number = v_sector_number
    ORDER BY piece_size DESC  -- Descending order for biggest size first

    LOOP
        -- Insert sorted data into sectors_snap_initial_pieces with updated piece_index
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
        VALUES (
                   v_sp_id,
                   v_sector_number,
                   new_index,  -- Insert new_index as piece_index
                   sorted_pieces.piece_cid,
                   sorted_pieces.piece_size,
                   sorted_pieces.data_url,
                   sorted_pieces.data_headers,
                   sorted_pieces.data_raw_size,
                   sorted_pieces.data_delete_on_finalize,
                   sorted_pieces.direct_start_epoch,
                   sorted_pieces.direct_end_epoch,
                   sorted_pieces.direct_piece_activation_manifest,
                   sorted_pieces.created_at
               );

        new_index := new_index + 1;
    END LOOP;

    -- Delete entries from open_sector_pieces after successful transfer
    IF FOUND THEN
        DELETE FROM open_sector_pieces
        WHERE sp_id = v_sp_id AND sector_number = v_sector_number;
    ELSE
        RAISE EXCEPTION 'No data found to transfer for sp_id % and sector_number %', v_sp_id, v_sector_number;
    END IF;

END;
$$ LANGUAGE plpgsql;
