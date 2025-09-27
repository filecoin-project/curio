-- Proofshare queue

CREATE TABLE IF NOT EXISTS proofshare_queue (
    service_id BIGINT NOT NULL,
    
    obtained_at TIMESTAMP WITH TIME ZONE NOT NULL,

    request_cid TEXT NOT NULL,
    response_data BYTEA,

    compute_task_id BIGINT,
    compute_done BOOLEAN NOT NULL DEFAULT FALSE,

    submit_task_id BIGINT,
    submit_done BOOLEAN NOT NULL DEFAULT FALSE,

    -- 20250620-proofshare-pow.sql
    -- was_pow BOOLEAN NOT NULL DEFAULT FALSE,

    PRIMARY KEY (service_id, obtained_at)
);

-- 20250619-proofshare-fixes.sql
-- create unique index proofshare_queue_request_cid_uindex
--    on proofshare_queue (request_cid);

CREATE TABLE IF NOT EXISTS proofshare_meta (
    singleton BOOLEAN NOT NULL DEFAULT TRUE CHECK (singleton = TRUE) UNIQUE,

    enabled BOOLEAN NOT NULL DEFAULT FALSE,

    wallet TEXT,
    pprice TEXT NOT NULL DEFAULT '0',

    request_task_id BIGINT,

    -- 20250724-proofshare-autosettle.sql
    -- autosettle BOOLEAN NOT NULL DEFAULT TRUE,

    PRIMARY KEY (singleton)
);

COMMENT ON COLUMN proofshare_meta.enabled IS 'Setting to TRUE indicates acceptance of provider TOS in lib/proofsvc/tos/provider.md and privacy.md';

INSERT INTO proofshare_meta (singleton, enabled, wallet) VALUES (TRUE, FALSE, NULL);

CREATE TABLE IF NOT EXISTS proofshare_provider_payments (
    provider_id BIGINT NOT NULL, -- wallet id
    request_cid TEXT NOT NULL,

    payment_nonce BIGINT NOT NULL,
    payment_cumulative_amount TEXT NOT NULL,
    payment_signature BYTEA NOT NULL,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,

    PRIMARY KEY (provider_id, payment_nonce)
);

create index if not exists proofshare_provider_payments_request_cid_index
    on proofshare_provider_payments (request_cid);

CREATE TABLE IF NOT EXISTS proofshare_provider_payments_settlement (
    provider_id BIGINT NOT NULL, -- wallet id
    payment_nonce BIGINT NOT NULL,

    settled_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    settle_message_cid TEXT NOT NULL,

    PRIMARY KEY (provider_id, payment_nonce)
);

-- Table tracking provider-router interactions (deposit, withdraw-request/complete)
CREATE TABLE IF NOT EXISTS proofshare_provider_messages (
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    signed_cid TEXT NOT NULL,

    wallet BIGINT NOT NULL,
    action TEXT,

    success BOOLEAN,
    completed_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (started_at, signed_cid)
);

CREATE INDEX IF NOT EXISTS proofshare_provider_messages_signed_cid ON proofshare_provider_messages (signed_cid);

-- Client settings

CREATE TABLE IF NOT EXISTS proofshare_client_settings (
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sp_id BIGINT NOT NULL DEFAULT 0, -- 0 = all/other

    wallet TEXT,

    minimum_pending_seconds BIGINT NOT NULL DEFAULT 0,
    
    do_porep BOOLEAN NOT NULL DEFAULT FALSE,
    do_snap BOOLEAN NOT NULL DEFAULT FALSE,

    pprice TEXT NOT NULL DEFAULT '0', -- attofil/proof

    PRIMARY KEY (sp_id)
);

COMMENT ON COLUMN proofshare_client_settings.enabled IS 'Setting to TRUE indicates acceptance of client TOS in lib/proofsvc/tos/client.md and privacy.md';

INSERT INTO proofshare_client_settings (enabled, sp_id, wallet, minimum_pending_seconds, do_porep, do_snap) VALUES (FALSE, 0, NULL, 0, FALSE, FALSE);

CREATE TABLE IF NOT EXISTS proofshare_client_requests (
    task_id BIGINT NOT NULL,
    
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,

    request_cid TEXT,
    request_uploaded BOOLEAN NOT NULL DEFAULT FALSE,
    request_partition_cost INTEGER NOT NULL DEFAULT 10,

    payment_wallet BIGINT,
    payment_nonce BIGINT,

    request_sent BOOLEAN NOT NULL DEFAULT FALSE,

    response_data BYTEA,

    done BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    done_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (task_id)
);

-- 20250801-proofshare-pipeline.sql rewrites the above to
--CREATE TABLE proofshare_client_requests (
--    sp_id BIGINT NOT NULL,
--    sector_num BIGINT NOT NULL,

--    request_type TEXT NOT NULL, -- "porep" or "snap"

--    task_id_upload BIGINT,
--    request_cid TEXT,
--    request_uploaded BOOLEAN NOT NULL DEFAULT FALSE,
--    request_partition_cost INTEGER NOT NULL DEFAULT 10,

--    payment_wallet BIGINT,
--    payment_nonce BIGINT,

--    request_sent BOOLEAN NOT NULL DEFAULT FALSE,

--    task_id_poll BIGINT,
--    response_data BYTEA,

--    done BOOLEAN NOT NULL DEFAULT FALSE,

--    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
--    done_at TIMESTAMP WITH TIME ZONE,

--    PRIMARY KEY (sp_id, sector_num, request_type)
--);
--CREATE TABLE proofshare_client_sender
-- (
--     wallet_id  BIGINT NOT NULL PRIMARY KEY,
--     task       BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,
--     updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp
-- );

CREATE TABLE IF NOT EXISTS proofshare_client_wallets (
    wallet BIGINT NOT NULL PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS proofshare_client_payments (
    wallet BIGINT NOT NULL,

    nonce BIGINT NOT NULL,
    cumulative_amount TEXT NOT NULL,

    signature BYTEA NOT NULL,

    consumed BOOLEAN NOT NULL DEFAULT FALSE,

    -- 20250728-proofshare-payment-stats.sql
    -- created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    -- consumed_at TIMESTAMP WITH TIME ZONE, (auto-set with a trigger when consumed is set to TRUE)

    PRIMARY KEY (wallet, nonce)
);

-- Table tracking user-router interactions (deposit, withdraw-request/complete)
CREATE TABLE IF NOT EXISTS proofshare_client_messages (
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    signed_cid TEXT NOT NULL,

    wallet BIGINT NOT NULL,
    action TEXT,

    success BOOLEAN,
    completed_at TIMESTAMP WITH TIME ZONE,

    PRIMARY KEY (started_at, signed_cid)
);

CREATE INDEX IF NOT EXISTS proofshare_client_messages_signed_cid ON proofshare_client_messages (signed_cid);

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

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'tr_update_proofshare_client_messages'
    ) THEN
        CREATE TRIGGER tr_update_proofshare_client_messages AFTER UPDATE ON message_waits
FOR EACH ROW
WHEN (OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL)
EXECUTE FUNCTION update_proofshare_client_messages_from_message_waits();
    END IF;
END $$;

CREATE OR REPLACE FUNCTION update_proofshare_provider_messages_from_message_waits()
RETURNS trigger AS $$
BEGIN
  IF OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL THEN
    UPDATE proofshare_provider_messages
      SET success = (NEW.executed_rcpt_exitcode = 0),
          completed_at = current_timestamp
    WHERE signed_cid = NEW.signed_message_cid;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger 
        WHERE tgname = 'tr_update_proofshare_provider_messages'
    ) THEN
        CREATE TRIGGER tr_update_proofshare_provider_messages AFTER UPDATE ON message_waits
FOR EACH ROW
WHEN (OLD.executed_tsk_epoch IS NULL AND NEW.executed_tsk_epoch IS NOT NULL)
EXECUTE FUNCTION update_proofshare_provider_messages_from_message_waits();
    END IF;
END $$;


