DROP INDEX IF EXISTS idx_parked_pieces_cleanup_eligible;

DROP TRIGGER IF EXISTS parked_piece_refs_insert_refcount ON parked_piece_refs;
DROP TRIGGER IF EXISTS parked_piece_refs_delete_refcount ON parked_piece_refs;
DROP TRIGGER IF EXISTS parked_piece_refs_update_refcount ON parked_piece_refs;

DROP FUNCTION IF EXISTS increment_parked_piece_ref_count();
DROP FUNCTION IF EXISTS decrement_parked_piece_ref_count();
DROP FUNCTION IF EXISTS adjust_parked_piece_ref_count_on_update();

ALTER TABLE parked_pieces DROP COLUMN IF EXISTS ref_count;
