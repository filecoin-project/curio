-- Piece files are found from the hash map: the range owner, or both ends
-- of an in-flight move. Range sizes are published from the disk by the
-- node that holds it.
ALTER TABLE hash_space_range ADD COLUMN IF NOT EXISTS size BIGINT NOT NULL DEFAULT 0;

DROP TABLE IF EXISTS open_piece;
