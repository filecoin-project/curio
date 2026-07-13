DO $$
BEGIN
    IF to_regclass('message_waits_eth') IS NOT NULL
       AND to_regclass('pdp_data_set_creates') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_constraint
           WHERE contype = 'f'
             AND conrelid = to_regclass('pdp_data_set_creates')
             AND confrelid = to_regclass('message_waits_eth')
       ) THEN
        ALTER TABLE pdp_data_set_creates
            ADD CONSTRAINT pdp_data_set_creates_create_message_hash_fkey
            FOREIGN KEY (create_message_hash)
            REFERENCES message_waits_eth(signed_tx_hash)
            ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('message_waits_eth') IS NOT NULL
       AND to_regclass('pdp_data_set_pieces') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_constraint
           WHERE contype = 'f'
             AND conrelid = to_regclass('pdp_data_set_pieces')
             AND confrelid = to_regclass('message_waits_eth')
       ) THEN
        ALTER TABLE pdp_data_set_pieces
            ADD CONSTRAINT pdp_data_set_pieces_add_message_hash_fkey
            FOREIGN KEY (add_message_hash)
            REFERENCES message_waits_eth(signed_tx_hash)
            ON DELETE CASCADE;
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('message_waits_eth') IS NOT NULL
       AND to_regclass('pdp_data_set_piece_adds') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
           FROM pg_constraint
           WHERE contype = 'f'
             AND conrelid = to_regclass('pdp_data_set_piece_adds')
             AND confrelid = to_regclass('message_waits_eth')
       ) THEN
        ALTER TABLE pdp_data_set_piece_adds
            ADD CONSTRAINT pdp_data_set_piece_adds_add_message_hash_fkey
            FOREIGN KEY (add_message_hash)
            REFERENCES message_waits_eth(signed_tx_hash)
            ON DELETE CASCADE;
    END IF;
END $$;
