ALTER TABLE ipni_peerid ADD UNIQUE (sp_id);

create unique index sectors_pipeline_events_task_history_id_uindex
    on sectors_pipeline_events (task_history_id, sp_id, sector_number);
