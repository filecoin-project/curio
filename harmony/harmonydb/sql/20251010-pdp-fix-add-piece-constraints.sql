-- changes an errand constraint from (data_set, add_message_hash, sub_piece_offset) to (data_set, add_message_hash, add_message_index)

ALTER TABLE pdp_data_set_piece_adds
  DROP CONSTRAINT pdp_data_set_piece_adds_piece_id_unique;

ALTER TABLE pdp_data_set_piece_adds
  ADD CONSTRAINT pdp_data_set_piece_adds_pk
  PRIMARY KEY (data_set HASH, add_message_hash ASC, add_message_index ASC);
