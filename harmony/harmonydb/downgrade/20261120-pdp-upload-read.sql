ALTER TABLE pdp_piece_pulls DROP COLUMN IF EXISTS read_restricted;
ALTER TABLE pdp_piece_streaming_uploads DROP COLUMN IF EXISTS read_restricted;
ALTER TABLE pdp_piece_uploads DROP COLUMN IF EXISTS read_restricted;
