ALTER TABLE harmony_task_history
    ADD COLUMN sp_id BIGINT DEFAULT NULL,
    ADD COLUMN sector_number BIGINT DEFAULT NULL;

CREATE INDEX harmony_task_history_sp_sector_index ON harmony_task_history (sp_id, sector_number);

CREATE TABLE sectors_task_events (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    events BYTEA ARRAY DEFAULT '{}',

    primary key (sp_id, sector_number)
);

CREATE OR REPLACE FUNCTION append_sector_event(sp_id_param BIGINT, sector_number_param BIGINT, new_event_param BYTEA) RETURNS void AS $$
BEGIN
    -- Check if the row exists
    IF NOT EXISTS (
        SELECT 1 FROM sectors_task_events
        WHERE sp_id = sp_id_param AND sector_number = sector_number_param
    ) THEN
        -- Insert a new row if it does not exist
        INSERT INTO sectors_task_events (sp_id, sector_number, events)
        VALUES (sp_id_param, sector_number_param, ARRAY[new_event_param]);
    ELSE
        -- Append the new event if the row exists
        UPDATE sectors_task_events
        SET events = array_append(events, new_event_param)
        WHERE sp_id = sp_id_param AND sector_number = sector_number_param;
    END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION get_sector_number_by_task_id(task_id BIGINT)
    RETURNS TABLE(sp_id BIGINT, sector_number BIGINT) AS $$
BEGIN
    RETURN QUERY
        SELECT s.sp_id, s.sector_number
        FROM sectors_sdr_pipeline s
        WHERE s.task_id_sdr = task_id
           OR s.task_id_tree_d = task_id
           OR s.task_id_tree_c = task_id
           OR s.task_id_tree_r = task_id
           OR s.task_id_precommit_msg = task_id
           OR s.task_id_porep = task_id
           OR s.task_id_finalize = task_id
           OR s.task_id_move_storage = task_id
           OR s.task_id_commit_msg = task_id
           OR s.task_id_synth = task_id;
END;
$$ LANGUAGE plpgsql;