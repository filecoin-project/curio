DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'filecoin_payment_transactions'
        AND table_schema = current_schema()
        AND column_name = 'settle_upto_epoch'
    ) THEN
    ALTER TABLE filecoin_payment_transactions ADD COLUMN IF NOT EXISTS settle_upto_epoch BIGINT;
    END IF;
END $$;