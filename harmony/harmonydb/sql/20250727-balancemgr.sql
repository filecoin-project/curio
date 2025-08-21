-- Balance Manager

CREATE TABLE balance_manager_addresses (
    id SERIAL PRIMARY KEY,
    
    subject_address TEXT NOT NULL, -- f0 address
    second_address TEXT NOT NULL, -- f0 address

    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_action TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- "requester" - if subject below low watermark, send fil from second address to subject up to high watermark
    -- "active-provider" - if subject above high watermark, send fil from subject to second address up to low watermark
    action_type TEXT NOT NULL, -- "requester", "active-provider"

    -- added in 20250817-balancemgr-pshare.sql
    -- subject_type TEXT NOT NULL DEFAULT 'wallet', -- "wallet", "proofshare"

    low_watermark_fil_balance TEXT NOT NULL DEFAULT '0',
    high_watermark_fil_balance TEXT NOT NULL DEFAULT '0',

    active_task_id BIGINT,

    last_msg_cid TEXT,
    last_msg_sent_at TIMESTAMP,
    last_msg_landed_at TIMESTAMP
);

CREATE INDEX balance_manager_addresses_last_msg_cid_idx ON balance_manager_addresses (last_msg_cid);

ALTER TABLE balance_manager_addresses ADD CONSTRAINT subject_not_equal_second CHECK (subject_address != second_address);
ALTER TABLE balance_manager_addresses ADD CONSTRAINT balance_manager_addresses_subject_address_second_address_unique UNIQUE (subject_address, second_address, action_type);

CREATE OR REPLACE FUNCTION update_balance_manager_from_message_waits()
RETURNS trigger AS $$
BEGIN
  IF OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL THEN
    UPDATE balance_manager_addresses
      SET last_msg_landed_at = current_timestamp
    WHERE last_msg_cid = NEW.signed_message_cid;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tr_update_balance_manager_from_message_waits
AFTER UPDATE ON message_waits
FOR EACH ROW
WHEN (OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL)
EXECUTE FUNCTION update_balance_manager_from_message_waits();
