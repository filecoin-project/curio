ALTER TABLE sectors_snap_pipeline
    ADD COLUMN failed BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE sectors_snap_pipeline
    ADD COLUMN failed_at TIMESTAMP WITH TIME ZONE;

ALTER TABLE sectors_snap_pipeline
    ADD COLUMN failed_reason VARCHAR(20) NOT NULL DEFAULT '';

ALTER TABLE sectors_snap_pipeline
    ADD COLUMN failed_reason_msg TEXT NOT NULL DEFAULT '';
