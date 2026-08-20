/*
  Streaming-upload timestamps represent absolute instants. Convert legacy
  naive columns, if present, by interpreting their stored values as UTC.
  Existing TIMESTAMPTZ columns already store absolute instants and are left
  unchanged.
*/

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'pdp_piece_streaming_uploads'
          AND column_name = 'created_at'
          AND data_type = 'timestamp without time zone'
    ) THEN
        ALTER TABLE pdp_piece_streaming_uploads
            ALTER COLUMN created_at DROP DEFAULT;
        ALTER TABLE pdp_piece_streaming_uploads
            ALTER COLUMN created_at TYPE TIMESTAMPTZ
            USING created_at AT TIME ZONE 'UTC';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'pdp_piece_streaming_uploads'
          AND column_name = 'completed_at'
          AND data_type = 'timestamp without time zone'
    ) THEN
        ALTER TABLE pdp_piece_streaming_uploads
            ALTER COLUMN completed_at TYPE TIMESTAMPTZ
            USING completed_at AT TIME ZONE 'UTC';
    END IF;
END $$;

ALTER TABLE pdp_piece_streaming_uploads
    ALTER COLUMN created_at SET DEFAULT NOW();
