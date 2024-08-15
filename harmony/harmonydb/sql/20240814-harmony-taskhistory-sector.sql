ALTER TABLE harmony_task_history
    ADD COLUMN sp_id BIGINT DEFAULT NULL,
    ADD COLUMN sector_number BIGINT DEFAULT NULL;

CREATE INDEX harmony_task_history_sp_sector_index ON harmony_task_history (sp_id, sector_number);
