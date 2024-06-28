ALTER TABLE sectors_sdr_pipeline
    ADD COLUMN task_id_synth bigint;

ALTER TABLE sectors_sdr_pipeline
    ADD COLUMN after_synth bool not null default false;