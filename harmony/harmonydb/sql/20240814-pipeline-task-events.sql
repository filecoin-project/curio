CREATE TABLE IF NOT EXISTS sectors_pipeline_events
(
    sp_id            BIGINT   NOT NULL,
    sector_number    BIGINT   NOT NULL,
    task_history_id  BIGINT   NOT NULL,

    PRIMARY KEY (sp_id, sector_number, task_history_id)
);

-- 20241106-market-fixes.sql
-- create unique index sectors_pipeline_events_task_history_id_uindex
--  on sectors_pipeline_events (task_history_id, sp_id, sector_number);

CREATE OR REPLACE FUNCTION append_sector_pipeline_events(
    sp_id_param BIGINT,
    sector_number_param BIGINT,
    task_history_id_param BIGINT
)
    RETURNS VOID AS $$
BEGIN
    INSERT INTO sectors_pipeline_events (sp_id, sector_number, task_history_id)
    VALUES (sp_id_param, sector_number_param, task_history_id_param)
    ON CONFLICT (sp_id, sector_number, task_history_id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;
