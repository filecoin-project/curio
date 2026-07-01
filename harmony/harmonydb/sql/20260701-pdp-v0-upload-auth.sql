-- Cached on-chain payer for local wallet-proof verification at upload time.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_sets'
          AND table_schema = current_schema()
          AND column_name = 'payer_address'
    ) THEN
        ALTER TABLE pdp_data_sets ADD COLUMN payer_address TEXT;
    END IF;
END
$$;

COMMENT ON COLUMN pdp_data_sets.payer_address IS 'Cached FWSS payer address for wallet-proof verification at upload time';

-- Pending payer captured at create time, copied into pdp_data_sets when the chain confirms.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'pdp_data_set_creates'
          AND table_schema = current_schema()
          AND column_name = 'payer_address'
    ) THEN
        ALTER TABLE pdp_data_set_creates ADD COLUMN payer_address TEXT;
    END IF;
END
$$;

COMMENT ON COLUMN pdp_data_set_creates.payer_address IS 'FWSS payer decoded from create extraData, pending data set confirmation';
