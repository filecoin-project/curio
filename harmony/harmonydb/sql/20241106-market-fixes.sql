ALTER TABLE ipni_peerid ADD UNIQUE (sp_id);

CREATE UNIQUE INDEX sectors_pipeline_events_task_history_id_uindex
    ON sectors_pipeline_events (task_history_id, sp_id, sector_number);

CREATE UNIQUE INDEX market_piece_deal_piece_cid_id_uindex
    ON market_piece_deal (piece_cid, id);

alter table market_mk12_deals
    add proposal_cid text not null;

CREATE INDEX market_mk12_deals_proposal_cid_index
    ON market_mk12_deals (proposal_cid);


-- Add the is_skip column to the ipni table
ALTER TABLE ipni
    ADD COLUMN is_skip BOOLEAN NOT NULL DEFAULT FALSE; -- set to true means return 404 for related entries

DROP INDEX IF EXISTS ipni_context_id;
CREATE INDEX ipni_context_id ON ipni(context_id, ad_cid, is_rm, is_skip);

CREATE INDEX ipni_entries_skip ON ipni(entries, is_skip, piece_cid);