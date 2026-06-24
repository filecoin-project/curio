DROP INDEX IF EXISTS idx_parked_pieces_task_id;
DROP INDEX IF EXISTS idx_parked_pieces_cleanup_task_id;
DROP INDEX IF EXISTS idx_sector_location_storage_id;
DROP INDEX IF EXISTS idx_pdp_data_set_pieces_data_set_add_msg;
DROP INDEX IF EXISTS idx_pdp_piecerefs_save_cache_pending;
DROP INDEX IF EXISTS idx_pdp_piecerefs_save_cache_task;
DROP INDEX IF EXISTS idx_message_sends_eth_reorg_check;
CREATE INDEX IF NOT EXISTS idx_message_sends_eth_reorg_check
    ON message_sends_eth (send_time, send_reason)
    WHERE send_success = TRUE AND send_time IS NOT NULL AND signed_hash IS NOT NULL;
