ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_commit_msg_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_finalize_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_move_storage_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_porep_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_precommit_msg_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_sdr_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_tree_c_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_tree_d_fkey;
ALTER TABLE sectors_sdr_pipeline DROP CONSTRAINT sectors_sdr_pipeline_task_id_tree_r_fkey;

ALTER TABLE parked_pieces DROP CONSTRAINT parked_pieces_cleanup_task_id_fkey;
ALTER TABLE parked_pieces DROP CONSTRAINT parked_pieces_task_id_fkey;
