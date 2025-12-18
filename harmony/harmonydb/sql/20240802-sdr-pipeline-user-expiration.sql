ALTER TABLE sectors_sdr_pipeline ADD COLUMN IF NOT EXISTS user_sector_duration_epochs BIGINT DEFAULT NULL;
