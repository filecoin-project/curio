DO $$
BEGIN
  IF POSITION('-YB-' IN version()) > 0 THEN
    IF EXISTS (
      SELECT 1
      FROM pg_constraint c
      WHERE c.conname = 'pdp_data_set_piece_adds_pk'
        AND c.conrelid = to_regclass('pdp_data_set_piece_adds')
        AND c.contype = 'p'
        AND pg_get_indexdef(c.conindid) LIKE
            '%USING lsm (add_message_hash HASH, add_message_index ASC)%'
    ) THEN
      ALTER TABLE pdp_data_set_piece_adds
        DROP CONSTRAINT pdp_data_set_piece_adds_pk;

      EXECUTE 'ALTER TABLE pdp_data_set_piece_adds
        ADD CONSTRAINT pdp_data_set_piece_adds_pk
        PRIMARY KEY (add_message_hash, add_message_index)';
    END IF;
  END IF;
END $$;