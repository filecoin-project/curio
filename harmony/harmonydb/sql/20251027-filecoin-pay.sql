CREATE TABLE IF NOT EXISTS filecoin_payment_transactions (
    tx_hash TEXT PRIMARY KEY,
    rail_ids BIGINT[] NOT NULL,
    settled_at BIGINT
);

CREATE TABLE IF NOT EXISTS pdp_delete_data_set (
    id SERIAL PRIMARY KEY,

    terminate_service_task_id BIGINT,
    after_terminate_service BOOLEAN NOT NULL DEFAULT FALSE,
    terminate_tx_hash TEXT,

    termination_epoch BIGINT,

    delete_data_set_task_id BIGINT NOT NULL,
    after_delete_data_set BOOLEAN NOT NULL DEFAULT FALSE,
    delete_tx_hash TEXT,

    terminated BOOLEAN NOT NULL DEFAULT FALSE
)