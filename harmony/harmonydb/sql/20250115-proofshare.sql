CREATE TABLE proofshare_queue (
    service_id BIGINT NOT NULL,
    
    obtained_at TIMESTAMPZ NOT NULL,

    request_data JSONB NOT NULL,
    response_data JSONB,

    compute_task_id BIGINT,
    compute_done BOOLEAN NOT NULL DEFAULT FALSE,

    submit_task_id BIGINT,
    submit_done BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (service_id, obtained_at)
);

CREATE TABLE proofshare_meta (
    singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton = TRUE) UNIQUE,

    enabled BOOLEAN NOT NULL DEFAULT FALSE,

    wallet TEXT,

    request_task_id BIGINT,

    PRIMARY KEY (singleton)
);

INSERT INTO proofshare_meta (singleton, enabled, wallet) VALUES (TRUE, FALSE, NULL);
