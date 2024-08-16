CREATE TABLE sectors_pipeline_events
(
    sp_id            BIGINT   NOT NULL,
    sector_number    BIGINT   NOT NULL,
    task_history_ids BIGINT[] NOT NULL,

    PRIMARY KEY (sp_id, sector_number)
);

CREATE OR REPLACE FUNCTION append_sector_pipeline_events(
    sp_id_param BIGINT,
    sector_number_param BIGINT,
    new_task_history_id_param BIGINT
)
    RETURNS VOID AS $$
BEGIN
    INSERT INTO sectors_pipeline_events (sp_id, sector_number, task_history_ids)
    VALUES (sp_id_param, sector_number_param, ARRAY[new_task_history_id_param])
    ON CONFLICT (sp_id, sector_number) DO UPDATE
        SET task_history_ids = array_append(sectors_pipeline_events.task_history_ids, new_task_history_id_param);
END;
$$ LANGUAGE plpgsql;