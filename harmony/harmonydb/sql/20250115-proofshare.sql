-- Proofshare queue

CREATE TABLE proofshare_queue (
    service_id BIGINT NOT NULL,
    
    obtained_at TIMESTAMP WITH TIME ZONE NOT NULL,

    request_cid TEXT NOT NULL,
    response_data BYTEA,

    compute_task_id BIGINT,
    compute_done BOOLEAN NOT NULL DEFAULT FALSE,

    submit_task_id BIGINT,
    submit_done BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (service_id, obtained_at)
);

CREATE TABLE proofshare_meta (
    singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton = TRUE) UNIQUE,

    enabled BOOLEAN NOT NULL DEFAULT FALSE,

    wallet TEXT,
    pprice TEXT NOT NULL DEFAULT '0',

    request_task_id BIGINT,

    PRIMARY KEY (singleton)
);

INSERT INTO proofshare_meta (singleton, enabled, wallet) VALUES (TRUE, FALSE, NULL);

-- Client settings

CREATE TABLE proofshare_client_settings (
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sp_id BIGINT NOT NULL DEFAULT 0, -- 0 = all/other

    wallet TEXT,

    minimum_pending_seconds BIGINT NOT NULL DEFAULT 0,
    
    do_porep BOOLEAN NOT NULL DEFAULT FALSE,
    do_snap BOOLEAN NOT NULL DEFAULT FALSE,

    pprice TEXT NOT NULL DEFAULT '0', -- attofil/proof

    PRIMARY KEY (sp_id)
);

INSERT INTO proofshare_client_settings (enabled, sp_id, wallet, minimum_pending_seconds, do_porep, do_snap) VALUES (FALSE, 0, NULL, 0, FALSE, FALSE);

CREATE TABLE proofshare_client_requests (
    task_id BIGINT NOT NULL,
    
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,

    service_id BIGINT NOT NULL,
    response_data BYTEA,

    done BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    done_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (task_id)
);

CREATE TABLE proofshare_client_wallets (
    wallet BIGINT NOT NULL PRIMARY KEY
);

CREATE TABLE proofshare_client_payments (
    wallet BIGINT NOT NULL,

    nonce BIGINT NOT NULL,
    cumulative_amount TEXT NOT NULL,

    signature BYTEA NOT NULL,

    consumed BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (wallet, nonce)
);

-- Table tracking user-router interactions (deposit, withdraw-request/complete)
CREATE TABLE proofshare_client_messages (
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    signed_cid TEXT NOT NULL,

    wallet BIGINT NOT NULL,
    action TEXT,

    success BOOLEAN,
    completed_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (started_at, signed_cid)
);

CREATE INDEX proofshare_client_messages_signed_cid ON proofshare_client_messages (signed_cid);

CREATE OR REPLACE FUNCTION update_proofshare_client_messages_from_message_waits()
RETURNS trigger AS $$
BEGIN
  IF OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL THEN
    UPDATE proofshare_client_messages
      SET success = (NEW.executed_rcpt_exitcode = 0),
          completed_at = current_timestamp
    WHERE signed_cid = NEW.signed_message_cid;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tr_update_proofshare_client_messages
AFTER UPDATE ON message_waits
FOR EACH ROW
WHEN (OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL)
EXECUTE FUNCTION update_proofshare_client_messages_from_message_waits();

