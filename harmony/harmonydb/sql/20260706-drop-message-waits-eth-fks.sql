-- message_waits_eth rows are confirmation records and must be prunable without
-- cascading into PDPv0 state/history tables.
ALTER TABLE pdp_data_set_creates
    DROP CONSTRAINT IF EXISTS pdp_proofset_creates_create_message_hash_fkey;

ALTER TABLE pdp_data_set_pieces
    DROP CONSTRAINT IF EXISTS pdp_proofset_roots_add_message_hash_fkey;

ALTER TABLE pdp_data_set_piece_adds
    DROP CONSTRAINT IF EXISTS pdp_proofset_root_adds_add_message_hash_fkey;
