CREATE TABLE batch_sector_refs (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,

    machine_host_and_port TEXT NOT NULL,
    pipeline_slot BIGINT NOT NULL,

    PRIMARY KEY (sp_id, sector_number, machine_host_and_port, pipeline_slot),
    FOREIGN KEY (sp_id, sector_number) REFERENCES sectors_sdr_pipeline (sp_id, sector_number)
);