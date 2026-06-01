-- Track parked_piece_refs reference count directly on parked_pieces.
--
-- The DropPiece cleanup task previously discovered eligible rows via an
-- anti-join against parked_piece_refs, which scaled poorly because most
-- parked_pieces rows have cleanup_task_id IS NULL. With ref_count maintained
-- by triggers, cleanup picks candidates from a tiny partial index.
--
-- Final cleanup DELETE remains guarded by a NOT EXISTS subquery so a race
-- between candidate selection and deletion cannot drop a piece that just
-- gained a new ref.

ALTER TABLE parked_pieces ADD COLUMN IF NOT EXISTS ref_count INTEGER NOT NULL DEFAULT 0;

-- Backfill ref_count from existing parked_piece_refs rows.
UPDATE parked_pieces pp
SET ref_count = sub.cnt
FROM (
    SELECT piece_id, COUNT(*)::int AS cnt
    FROM parked_piece_refs
    GROUP BY piece_id
) sub
WHERE pp.id = sub.piece_id
  AND pp.ref_count <> sub.cnt;

CREATE OR REPLACE FUNCTION increment_parked_piece_ref_count()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE parked_pieces
    SET ref_count = ref_count + 1
    WHERE id = NEW.piece_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION decrement_parked_piece_ref_count()
    RETURNS TRIGGER AS $$
BEGIN
    UPDATE parked_pieces
    SET ref_count = ref_count - 1
    WHERE id = OLD.piece_id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION adjust_parked_piece_ref_count_on_update()
    RETURNS TRIGGER AS $$
BEGIN
    IF OLD.piece_id IS DISTINCT FROM NEW.piece_id THEN
        UPDATE parked_pieces
        SET ref_count = ref_count - 1
        WHERE id = OLD.piece_id;
        UPDATE parked_pieces
        SET ref_count = ref_count + 1
        WHERE id = NEW.piece_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'parked_piece_refs_insert_refcount'
    ) THEN
        CREATE TRIGGER parked_piece_refs_insert_refcount
            AFTER INSERT ON parked_piece_refs
            FOR EACH ROW
            EXECUTE FUNCTION increment_parked_piece_ref_count();
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'parked_piece_refs_delete_refcount'
    ) THEN
        CREATE TRIGGER parked_piece_refs_delete_refcount
            AFTER DELETE ON parked_piece_refs
            FOR EACH ROW
            EXECUTE FUNCTION decrement_parked_piece_ref_count();
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'parked_piece_refs_update_refcount'
    ) THEN
        CREATE TRIGGER parked_piece_refs_update_refcount
            AFTER UPDATE ON parked_piece_refs
            FOR EACH ROW
            EXECUTE FUNCTION adjust_parked_piece_ref_count_on_update();
    END IF;
END $$;

-- Partial index for cleanup candidate scans. Indexes only the rows the
-- cleanup poller cares about, keeping the index tiny under steady-state load.
CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_eligible
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL AND ref_count = 0;
