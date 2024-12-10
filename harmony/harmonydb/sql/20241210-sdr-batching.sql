ALTER TABLE sectors_sdr_pipeline ADD COLUMN precommit_ready_at TIMESTAMPTZ;
ALTER TABLE sectors_sdr_pipeline ADD COLUMN commit_ready_at TIMESTAMPTZ;
