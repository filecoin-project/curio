-- Indexes for PDPv0 reorg check candidate selection (message_sends_eth + message_waits_eth).

-- Drive branch 1 from confirmed waits past finality depth.
CREATE INDEX IF NOT EXISTS idx_message_waits_eth_reorg_confirmed
    ON message_waits_eth (confirmed_block_number, signed_tx_hash)
    WHERE tx_status = 'confirmed'
      AND tx_success = TRUE
      AND confirmed_block_number IS NOT NULL;

-- Time-window scan of successful sends for reorg check.
CREATE INDEX IF NOT EXISTS idx_message_sends_eth_reorg_check
    ON message_sends_eth (send_time, send_reason)
    WHERE send_success = TRUE
      AND send_time IS NOT NULL
      AND signed_hash IS NOT NULL;

-- Join send rows to wait rows by normalized tx hash.
CREATE INDEX IF NOT EXISTS idx_message_sends_eth_signed_hash_norm
    ON message_sends_eth (LOWER(TRIM(BOTH FROM signed_hash)))
    WHERE send_success = TRUE
      AND signed_hash IS NOT NULL;
