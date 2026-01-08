ALTER TABLE pdp_data_set_pieces ADD COLUMN rm_message_hash TEXT DEFAULT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'terminate_service_task_id'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS terminate_service_task_id BIGINT DEFAULT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'after_terminate_service'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS after_terminate_service BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'terminate_tx_hash'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS terminate_tx_hash TEXT DEFAULT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'service_termination_epoch'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS service_termination_epoch BIGINT DEFAULT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'after_delete_data_set'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS after_delete_data_set BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_delete'
        AND column_name = 'terminated'
    ) THEN
    ALTER TABLE pdp_data_set_delete ADD COLUMN IF NOT EXISTS terminated BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;


DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'pdp_data_set_delete_set_id_unique'
    ) THEN
    ALTER TABLE pdp_data_set_delete
    ADD CONSTRAINT pdp_data_set_delete_set_id_unique UNIQUE (set_id);
    END IF;
END$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'pdp_piece_delete'
        AND column_name = 'terminated'
    ) THEN
    ALTER TABLE pdp_piece_delete ADD COLUMN IF NOT EXISTS terminated BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END $$;


