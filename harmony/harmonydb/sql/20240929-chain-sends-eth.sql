-- SQL Schema for message_sends_eth and message_send_eth_locks tables

CREATE TABLE message_sends_eth
(
    from_address  TEXT   NOT NULL,
    to_address    TEXT   NOT NULL,
    send_reason   TEXT   NOT NULL,
    send_task_id  SERIAL PRIMARY KEY,

    unsigned_tx   BYTEA  NOT NULL,
    unsigned_hash TEXT   NOT NULL,

    nonce         BIGINT,
    signed_tx     BYTEA,
    signed_hash   TEXT,

    send_time     TIMESTAMP DEFAULT NULL,
    send_success  BOOLEAN   DEFAULT NULL,
    send_error    TEXT
);

COMMENT ON COLUMN message_sends_eth.from_address IS 'Ethereum 0x... address';
COMMENT ON COLUMN message_sends_eth.to_address IS 'Ethereum 0x... address';
COMMENT ON COLUMN message_sends_eth.send_reason IS 'Optional description of send reason';
COMMENT ON COLUMN message_sends_eth.send_task_id IS 'Task ID of the send task';

COMMENT ON COLUMN message_sends_eth.unsigned_tx IS 'Unsigned transaction data';
COMMENT ON COLUMN message_sends_eth.unsigned_hash IS 'Hash of the unsigned transaction';

COMMENT ON COLUMN message_sends_eth.nonce IS 'Assigned transaction nonce, set while the send task is executing';
COMMENT ON COLUMN message_sends_eth.signed_tx IS 'Signed transaction data, set while the send task is executing';
COMMENT ON COLUMN message_sends_eth.signed_hash IS 'Hash of the signed transaction';

COMMENT ON COLUMN message_sends_eth.send_time IS 'Time when the send task was executed, set after pushing the transaction to the network';
COMMENT ON COLUMN message_sends_eth.send_success IS 'Whether this transaction was broadcasted to the network already, NULL if not yet attempted, TRUE if successful, FALSE if failed';
COMMENT ON COLUMN message_sends_eth.send_error IS 'Error message if send_success is FALSE';

CREATE UNIQUE INDEX message_sends_eth_success_index
    ON message_sends_eth (from_address, nonce)
    WHERE send_success IS NOT FALSE;

COMMENT ON INDEX message_sends_eth_success_index IS
    'message_sends_eth_success_index enforces sender/nonce uniqueness, it is a conditional index that only indexes rows where send_success is not false. This allows us to have multiple rows with the same sender/nonce, as long as only one of them was successfully broadcasted (true) to the network or is in the process of being broadcasted (null).';

CREATE TABLE message_send_eth_locks
(
    from_address TEXT      NOT NULL,
    task_id      BIGINT    NOT NULL,
    claimed_at   TIMESTAMP NOT NULL,

    CONSTRAINT message_send_eth_locks_pk
        PRIMARY KEY (from_address)
);
