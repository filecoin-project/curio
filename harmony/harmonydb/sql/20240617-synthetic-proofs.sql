ALTER TABLE sectors_sdr_pipeline ADD COLUMN IF NOT EXISTS task_id_synth bigint;

ALTER TABLE sectors_sdr_pipeline ADD COLUMN IF NOT EXISTS after_synth bool not null default false;