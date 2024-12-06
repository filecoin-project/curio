-- this migration fixes piecepark unique constraint issue to allow multiple pieceparks with same name when cleanup task is set

ALTER TABLE parked_pieces DROP CONSTRAINT IF EXISTS parked_pieces_piece_cid_key;
ALTER TABLE parked_pieces ADD CONSTRAINT parked_pieces_piece_cid_cleanup_task_id_key UNIQUE (piece_cid, cleanup_task_id);
