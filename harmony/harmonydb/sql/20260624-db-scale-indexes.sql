CREATE INDEX IF NOT EXISTS idx_parked_pieces_task_id
    ON parked_pieces (task_id) WHERE task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_task_id
    ON parked_pieces (cleanup_task_id) WHERE cleanup_task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sector_location_storage_id
    ON sector_location (storage_id);

CREATE INDEX IF NOT EXISTS idx_pdp_data_set_pieces_data_set_add_msg
    ON pdp_data_set_pieces (data_set, add_message_hash);

CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_save_cache_pending
    ON pdp_piecerefs (created_at) WHERE save_cache_task_id IS NULL AND needs_save_cache = TRUE;

CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_save_cache_task
    ON pdp_piecerefs (save_cache_task_id) WHERE save_cache_task_id IS NOT NULL;

DROP INDEX IF EXISTS idx_message_sends_eth_reorg_check;
CREATE INDEX IF NOT EXISTS idx_message_sends_eth_reorg_check
    ON message_sends_eth (send_reason, send_time)
    WHERE send_success = TRUE AND send_time IS NOT NULL AND signed_hash IS NOT NULL;
