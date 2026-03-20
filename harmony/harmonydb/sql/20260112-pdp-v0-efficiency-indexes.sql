-- Efficiency indexes for PDP and task polling - to reduce high CPU on YugabyteDB clusters
-- during backlog handling.

-- 1. pdp_piece_uploads: SELECT pu.id FROM pdp_piece_uploads pu JOIN parked_piece_refs pr...
--    WHERE pu.piece_ref IS NOT NULL AND pp.complete = TRUE AND pu.notify_task_id IS NULL
--    This is the #1 slow query (40-65% of total DB time)
CREATE INDEX IF NOT EXISTS idx_pdp_piece_uploads_notify_pending
    ON pdp_piece_uploads (piece_ref)
    WHERE notify_task_id IS NULL AND piece_ref IS NOT NULL;

-- 2. harmony_task: SELECT id, update_time, retries FROM harmony_task 
--    WHERE owner_id IS NULL AND name=$1 ORDER BY update_time
--    This query runs constantly for task polling (25-38% of total DB time)
CREATE INDEX IF NOT EXISTS idx_harmony_task_unowned_by_name
    ON harmony_task (name, update_time)
    WHERE owner_id IS NULL;

-- 3. parked_pieces long_term fetch: SELECT id FROM parked_pieces
--    WHERE long_term = $1 AND complete = FALSE AND task_id IS NULL
--    High frequency query (5-17% of total DB time)
CREATE INDEX IF NOT EXISTS idx_parked_pieces_incomplete_fetch
    ON parked_pieces (long_term)
    WHERE complete = FALSE AND task_id IS NULL;

-- 4. parked_pieces cleanup: SELECT pp.id FROM parked_pieces pp
--    WHERE pp.cleanup_task_id IS NULL AND NOT EXISTS (SELECT 1 FROM parked_piece_refs pr WHERE pr.piece_id = pp.id)
--    Anti-join pattern benefits from covering index (3-10% of total DB time)
--    Note: idx_parked_piece_refs_piece_id should already exist from 20251014-park-piece-optimisation.sql
CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_pending
    ON parked_pieces (id)
    WHERE cleanup_task_id IS NULL;

-- 5. message_sends_eth nonce lookup: SELECT MAX(nonce) FROM message_sends_eth
--    WHERE from_address = $1 AND send_success = TRUE
--    Used during ETH message sending (1-12% of total DB time)
CREATE INDEX IF NOT EXISTS idx_message_sends_eth_nonce_lookup
    ON message_sends_eth (from_address, nonce DESC)
    WHERE send_success = TRUE;

-- 6. pdp_piecerefs indexing task selection: SELECT id FROM pdp_piecerefs
--    WHERE indexing_task_id IS NULL AND needs_indexing = TRUE ORDER BY created_at ASC LIMIT...
CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_indexing_pending
    ON pdp_piecerefs (created_at ASC)
    WHERE indexing_task_id IS NULL AND needs_indexing = TRUE;

-- 7. pdp_piecerefs IPNI task selection: SELECT id FROM pdp_piecerefs
--    WHERE ipni_task_id IS NULL AND needs_ipni = TRUE ORDER BY created_at ASC LIMIT...
CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_ipni_pending
    ON pdp_piecerefs (created_at ASC)
    WHERE ipni_task_id IS NULL AND needs_ipni = TRUE;

-- 8. message_waits_eth pending selection: SELECT signed_tx_hash FROM message_waits_eth
--    WHERE waiter_machine_id = $1 AND tx_status = 'pending' LIMIT...
CREATE INDEX IF NOT EXISTS idx_message_waits_eth_waiter_pending
    ON message_waits_eth (waiter_machine_id, signed_tx_hash)
    WHERE tx_status = 'pending';

-- 9. pdp_data_set_piece_adds processing: SELECT DISTINCT data_set, add_message_hash
--    FROM pdp_data_set_piece_adds WHERE add_message_ok = TRUE AND pieces_added = FALSE
CREATE INDEX IF NOT EXISTS idx_pdp_data_set_piece_adds_unprocessed
    ON pdp_data_set_piece_adds (data_set, add_message_hash)
    WHERE add_message_ok = TRUE AND pieces_added = FALSE;

-- 10. pdp_data_set_pieces removal tracking: SELECT ... FROM pdp_data_set_pieces psp
--     WHERE psp.rm_message_hash IS NOT NULL AND psp.removed = FALSE
CREATE INDEX IF NOT EXISTS idx_pdp_data_set_pieces_pending_removal
    ON pdp_data_set_pieces (data_set, piece_id)
    WHERE rm_message_hash IS NOT NULL AND removed = FALSE;
    
