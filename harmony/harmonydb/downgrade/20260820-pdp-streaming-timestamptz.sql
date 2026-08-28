-- The canonical pre-migration schema already used TIMESTAMPTZ for both
-- columns, so retain their types and restore only the previous default.
ALTER TABLE pdp_piece_streaming_uploads
    ALTER COLUMN created_at SET DEFAULT TIMEZONE('UTC', NOW());
