-- PDP Termination Handling
-- Tracks terminated datasets and applies backoff for contract reverts.

-- Add termination and failure tracking columns to pdp_data_sets
ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS terminated_at_epoch BIGINT;
ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS consecutive_prove_failures INT NOT NULL DEFAULT 0;
ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS next_prove_attempt_at BIGINT;

COMMENT ON COLUMN pdp_data_sets.terminated_at_epoch IS 'Block height at which dataset termination was detected; NULL if active';
COMMENT ON COLUMN pdp_data_sets.consecutive_prove_failures IS 'Number of consecutive proving failures (resets on success)';
COMMENT ON COLUMN pdp_data_sets.next_prove_attempt_at IS 'Block height before which proving should not be attempted (backoff)';
