ALTER TABLE ipni_peerid ADD UNIQUE (sp_id);

CREATE UNIQUE INDEX sectors_pipeline_events_task_history_id_uindex
    ON sectors_pipeline_events (task_history_id, sp_id, sector_number);

CREATE UNIQUE INDEX market_piece_deal_piece_cid_id_uindex
    ON market_piece_deal (piece_cid, id);