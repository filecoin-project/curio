ALTER TABLE harmony_task_history
    ADD COLUMN sp_id BIGINT DEFAULT NULL,
    ADD COLUMN sector_number BIGINT DEFAULT NULL;

CREATE INDEX harmony_task_history_sp_sector_index ON harmony_task_history (sp_id, sector_number);

CREATE OR REPLACE FUNCTION get_sector_number_by_task_id(task_id_param BIGINT)
    RETURNS TABLE(sp_id BIGINT, sector_number BIGINT) AS $$
BEGIN
    RETURN QUERY
        SELECT s.sp_id, s.sector_number
        FROM sectors_sdr_pipeline s
        WHERE s.task_id_sdr = task_id_param
           OR s.task_id_tree_d = task_id_param
           OR s.task_id_tree_c = task_id_param
           OR s.task_id_tree_r = task_id_param
           OR s.task_id_precommit_msg = task_id_param
           OR s.task_id_porep = task_id_param
           OR s.task_id_finalize = task_id_param
           OR s.task_id_move_storage = task_id_param
           OR s.task_id_commit_msg = task_id_param
           OR s.task_id_synth = task_id_param;
END;
$$ LANGUAGE plpgsql;