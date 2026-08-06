/*
  Fix: message_sends_eth.send_time and message_send_eth_locks.claimed_at are
  TIMESTAMP WITHOUT TIME ZONE, written with CURRENT_TIMESTAMP.

  CURRENT_TIMESTAMP returns TIMESTAMPTZ; assigning it to a naive TIMESTAMP stores
  the session-local wall-clock value. When Go/pgx later reads that naive value, it
  treats the digits as UTC. In a non-UTC session (e.g. Asia/Shanghai), every
  timestamp appears shifted forward by the session UTC offset.

  message_sends / message_send_locks were converted in 20240522-ts-to-timestampz.sql;
  the ETH tables were created later (20240929) and never migrated.

  Conversion uses the session TimeZone so existing wall-clock values are
  interpreted in the same zone they were written under. After this change,
  CURRENT_TIMESTAMP writes store absolute instants correctly regardless of session TZ.
*/

ALTER TABLE message_sends_eth
ALTER COLUMN send_time TYPE TIMESTAMPTZ
    USING send_time AT TIME ZONE current_setting('TimeZone');

ALTER TABLE message_send_eth_locks
ALTER COLUMN claimed_at TYPE TIMESTAMPTZ
    USING claimed_at AT TIME ZONE current_setting('TimeZone');
