-- Retry failed DropPiece tasks with their original IDs to preserve parked-piece claims.
INSERT INTO harmony_task (id, name, added_by, posted_time)
SELECT DISTINCT p.cleanup_task_id, 'DropPiece', 0, CURRENT_TIMESTAMP
FROM parked_pieces AS p
WHERE p.cleanup_task_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM harmony_task AS t WHERE t.id = p.cleanup_task_id)
  AND EXISTS (
    SELECT 1 FROM harmony_task_history AS h
    WHERE h.task_id = p.cleanup_task_id
      AND h.name = 'DropPiece'
      AND h.result = FALSE
      AND h.err = 'error: cleaning up parked piece: lost ownership of piece cleanup task ' || p.cleanup_task_id::text
  )
ON CONFLICT (id) DO NOTHING;
