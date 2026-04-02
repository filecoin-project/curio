CREATE TABLE IF NOT EXISTS filecoin_payment_transactions (
    tx_hash TEXT PRIMARY KEY,
    rail_ids BIGINT[] NOT NULL
);

CREATE TABLE IF NOT EXISTS pdp_delete_data_set (
    id BIGINT PRIMARY KEY,

    terminate_service_task_id BIGINT,
    after_terminate_service BOOLEAN NOT NULL DEFAULT FALSE,
    terminate_tx_hash TEXT,

    service_termination_epoch BIGINT,

    delete_data_set_task_id BIGINT NOT NULL,
    after_delete_data_set BOOLEAN NOT NULL DEFAULT FALSE,
    delete_tx_hash TEXT,

    terminated BOOLEAN NOT NULL DEFAULT FALSE
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_pieces'
          AND table_schema = current_schema()
          AND column_name = 'rm_message_hash'
    ) THEN
        ALTER TABLE pdp_data_set_pieces ADD COLUMN IF NOT EXISTS rm_message_hash TEXT DEFAULT NULL;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_pieces'
          AND table_schema = current_schema()
          AND column_name = 'removed'
    ) THEN
        ALTER TABLE pdp_data_set_pieces ADD COLUMN IF NOT EXISTS removed BOOLEAN DEFAULT FALSE;
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS pdp_piecerefs_piece_cid_idx ON pdp_piecerefs (piece_cid);
