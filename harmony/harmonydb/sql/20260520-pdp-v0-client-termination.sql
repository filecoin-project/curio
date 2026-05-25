-- Track whether a pdpv0 service termination was requested by a client-facing
-- HTTP request or by Curio's internal/provider cleanup flow.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'client_requested_termination'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN client_requested_termination BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'termination_requested_at'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN termination_requested_at TIMESTAMPTZ;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'termination_extra_data'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN termination_extra_data BYTEA;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'client_terminate_service_task_id'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN client_terminate_service_task_id BIGINT;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'cleanup_pieces_task_id'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN cleanup_pieces_task_id BIGINT;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_delete_data_set'
          AND table_schema = current_schema()
          AND column_name = 'cleanup_pieces_tx_hash'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD COLUMN cleanup_pieces_tx_hash TEXT;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'pdp_delete_data_set_client_terminate_task_fk'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD CONSTRAINT pdp_delete_data_set_client_terminate_task_fk
            FOREIGN KEY (client_terminate_service_task_id)
            REFERENCES harmony_task(id)
            ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'pdp_delete_data_set_client_extra_data_check'
    ) THEN
        ALTER TABLE pdp_delete_data_set
            ADD CONSTRAINT pdp_delete_data_set_client_extra_data_check
            CHECK (
                client_requested_termination = FALSE
                OR (
                    termination_extra_data IS NOT NULL
                    AND octet_length(termination_extra_data) > 0
                )
            );
    END IF;
END
$$;
