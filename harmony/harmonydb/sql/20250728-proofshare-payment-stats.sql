-- Proofshare payment statistics upgrade

-- 1. Add timestamps to proofshare_client_payments table
ALTER TABLE proofshare_client_payments
    ADD COLUMN created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp;
ALTER TABLE proofshare_client_payments
    ADD COLUMN consumed_at TIMESTAMP WITH TIME ZONE;

-- 2. Create index on created_at
CREATE INDEX IF NOT EXISTS idx_proofshare_client_payments_created_at ON proofshare_client_payments (created_at ASC);

-- 3. Back-fill consumed_at for rows that were already consumed
UPDATE proofshare_client_payments
SET consumed_at = COALESCE(consumed_at, current_timestamp)
WHERE consumed = TRUE
  AND consumed_at IS NULL;

-- 4. Trigger to automatically set consumed_at when the payment becomes consumed
CREATE OR REPLACE FUNCTION trg_set_consumed_at_ps_client_payments()
RETURNS TRIGGER AS $$
BEGIN
    -- On INSERT: if the new row is already consumed, fill consumed_at
    IF (TG_OP = 'INSERT') THEN
        IF NEW.consumed = TRUE AND NEW.consumed_at IS NULL THEN
            NEW.consumed_at := current_timestamp;
        END IF;
        RETURN NEW;
    END IF;

    -- On UPDATE: if consumed changed from FALSE to TRUE, set consumed_at
    IF (TG_OP = 'UPDATE') THEN
        IF NEW.consumed = TRUE AND (OLD.consumed IS DISTINCT FROM TRUE) THEN
            NEW.consumed_at := current_timestamp;
        END IF;
        RETURN NEW;
    END IF;

    RETURN NEW; -- Fallback (should not reach here)
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tr_set_consumed_at_ps_client_payments ON proofshare_client_payments;
CREATE TRIGGER tr_set_consumed_at_ps_client_payments
BEFORE INSERT OR UPDATE ON proofshare_client_payments
FOR EACH ROW
EXECUTE FUNCTION trg_set_consumed_at_ps_client_payments();
