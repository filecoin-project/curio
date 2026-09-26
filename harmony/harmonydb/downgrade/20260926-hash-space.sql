DROP TRIGGER IF EXISTS pdp_piecerefs_hash_space_place ON pdp_piecerefs;
DROP FUNCTION IF EXISTS enqueue_hash_space_place();

DROP TABLE IF EXISTS hash_space_delete;
DROP TABLE IF EXISTS hash_space_place;
DROP TABLE IF EXISTS open_piece;
DROP TABLE IF EXISTS hash_space_pending_event;
DROP TABLE IF EXISTS hash_space_move_source;
DROP TABLE IF EXISTS hash_space_range;
DROP TABLE IF EXISTS hash_space_disk;
DROP TABLE IF EXISTS hash_space_meta;
