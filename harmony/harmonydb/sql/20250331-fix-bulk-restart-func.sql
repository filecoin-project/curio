CREATE OR REPLACE FUNCTION unset_task_id(sp_id_param bigint, sector_number_param bigint)
    RETURNS void AS $$
DECLARE
column_name text;
    column_names text[] := ARRAY[
        'task_id_sdr',
        'task_id_tree_d',
        'task_id_tree_c',
        'task_id_tree_r',
        'task_id_precommit_msg',
        'task_id_porep',
        'task_id_finalize',
        'task_id_move_storage',
        'task_id_commit_msg'
        ];
    update_query text;
    task_ids bigint[];
    task_id bigint;
BEGIN
    -- Get all non-null task IDs
    task_ids := get_sdr_pipeline_tasks(sp_id_param, sector_number_param);

    IF task_ids IS NULL OR array_length(task_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    -- Loop through each task ID and each column
    FOREACH column_name IN ARRAY column_names LOOP
            FOREACH task_id IN ARRAY task_ids LOOP
                    update_query := format('UPDATE sectors_sdr_pipeline SET %I = NULL WHERE %I = $1 AND sp_id = $2 AND sector_number = $3', column_name, column_name);
                    EXECUTE update_query USING task_id, sp_id_param, sector_number_param;
            END LOOP;
    END LOOP;
END;
$$ LANGUAGE plpgsql;


CREATE OR REPLACE FUNCTION unset_task_id_snap(sp_id_param bigint, sector_number_param bigint)
RETURNS void AS $$
DECLARE
column_name text;
    column_names text[] := ARRAY[
        'task_id_encode',
        'task_id_prove',
        'task_id_submit',
        'task_id_move_storage'
        ];
    update_query text;
    task_ids bigint[];
    task_id bigint;
BEGIN
    -- Get all non-null task IDs
    task_ids := get_snap_pipeline_tasks(sp_id_param, sector_number_param);

    IF task_ids IS NULL OR array_length(task_ids, 1) IS NULL THEN
        RETURN;
    END IF;

    -- Loop through each task ID and each column
    FOREACH column_name IN ARRAY column_names LOOP
            FOREACH task_id IN ARRAY task_ids LOOP
                    update_query := format('UPDATE sectors_snap_pipeline SET %I = NULL WHERE %I = $1 AND sp_id = $2 AND sector_number = $3', column_name, column_name);
                    EXECUTE update_query USING task_id, sp_id_param, sector_number_param;
            END LOOP;
    END LOOP;
END;
$$ LANGUAGE plpgsql;