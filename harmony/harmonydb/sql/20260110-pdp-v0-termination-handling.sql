-- PDP Termination Handling
-- Tracks terminated datasets and applies backoff for contract reverts.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND table_schema = current_schema()
          AND column_name = 'terminated_at_epoch'
    ) THEN
        ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS terminated_at_epoch BIGINT;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND table_schema = current_schema()
          AND column_name = 'consecutive_prove_failures'
    ) THEN
        ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS consecutive_prove_failures INT NOT NULL DEFAULT 0;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND table_schema = current_schema()
          AND column_name = 'next_prove_attempt_at'
    ) THEN
        ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS next_prove_attempt_at BIGINT;
    END IF;
END
$$;

COMMENT ON COLUMN pdp_data_sets.terminated_at_epoch IS 'Block height at which dataset termination was detected; NULL if active';
COMMENT ON COLUMN pdp_data_sets.consecutive_prove_failures IS 'Number of consecutive proving failures (resets on success)';
COMMENT ON COLUMN pdp_data_sets.next_prove_attempt_at IS 'Block height before which proving should not be attempted (backoff)';
