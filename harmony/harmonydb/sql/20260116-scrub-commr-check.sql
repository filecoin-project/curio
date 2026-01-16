-- Scrub CommR check table for verifying sealed/update sector files
CREATE TABLE IF NOT EXISTS scrub_commr_check (
    check_id BIGSERIAL PRIMARY KEY,

    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,

    task_id BIGINT,

    -- Which file type to check: 'sealed' or 'update'
    file_type TEXT NOT NULL DEFAULT 'sealed',
    
    -- Expected CommR (from chain or sector metadata)
    expected_comm_r TEXT NOT NULL,

    -- Results
    ok BOOLEAN,
    actual_comm_r TEXT,
    message TEXT,

    UNIQUE (sp_id, sector_number, create_time),
    UNIQUE (task_id)
);
