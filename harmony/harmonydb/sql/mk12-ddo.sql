ALTER TABLE market_mk12_deal_pipeline
    ADD COLUMN is_ddo BOOLEAN DEFAULT FALSE NOT NULL;

ALTER TABLE market_direct_deals
    ADD CONSTRAINT unique_sp_allocation UNIQUE (sp_id, allocation_id);

