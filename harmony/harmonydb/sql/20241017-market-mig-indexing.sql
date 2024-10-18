-- All indexing task entries are made into this table
-- and then copied over to market_mk12_deal_pipeline for a controlled migration
CREATE TABLE market_mk12_deal_pipeline_migration (
    uuid TEXT NOT NULL PRIMARY KEY,
    sp_id BIGINT NOT NULL,
    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL, -- padded size
    raw_size BIGINT DEFAULT NULL,
    sector BIGINT DEFAULT NULL,
    reg_seal_proof INT DEFAULT NULL,
    sector_offset BIGINT DEFAULT NULL, -- padded offset
    should_announce BOOLEAN NOT NULL
);

CREATE OR REPLACE FUNCTION migrate_deal_pipeline_entries()
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    -- Counts for existing entries and tasks
    num_entries_ready_for_indexing INTEGER;
    num_pending_ipni_tasks INTEGER;
    num_entries_needed INTEGER;
    num_entries_to_move INTEGER;
    cnt INTEGER;
    moved_rows RECORD;
BEGIN
    -- Step 1: Check if the migration table has entries
    SELECT COUNT(*) INTO cnt FROM market_mk12_deal_pipeline_migration LIMIT 16;
    IF cnt = 0 THEN
            RETURN;
    END IF;

    -- Step 2: Count entries ready for indexing in the pipeline table
    SELECT COUNT(*) INTO num_entries_ready_for_indexing
    FROM market_mk12_deal_pipeline
    WHERE sealed = TRUE AND should_index = TRUE AND indexed = FALSE LIMIT 16;

    -- Step 3: Count pending IPNI tasks
    SELECT COUNT(*) INTO num_pending_ipni_tasks
    FROM harmony_task
    WHERE name = 'IPNI' AND owner_id IS NULL LIMIT 16;

    -- Step 4: Calculate how many entries we need to reach 16
    num_entries_needed := 16 - num_entries_ready_for_indexing;

    -- If we already have 16 or more entries ready, no need to move more
    IF num_entries_needed <= 0 THEN
        RETURN;
    END IF;

    -- Step 5: Calculate how many entries we can move without exceeding 16 pending IPNI tasks
    num_entries_to_move := LEAST(num_entries_needed, 16 - num_pending_ipni_tasks);

    -- Limit by the number of entries available in the migration table
    SELECT COUNT(*) INTO cnt FROM market_mk12_deal_pipeline_migration LIMIT 16;
    num_entries_to_move := LEAST(num_entries_to_move, cnt);

    -- If no entries to move after calculations, exit
    IF num_entries_to_move <= 0 THEN
        RETURN;
    END IF;

    -- Move entries from the migration table to the pipeline table
    FOR moved_rows IN
    SELECT uuid, sp_id, piece_cid, piece_size, raw_size, sector, reg_seal_proof, sector_offset, should_announce
    FROM market_mk12_deal_pipeline_migration
             LIMIT num_entries_to_move
    LOOP
        -- Insert into the pipeline table
        INSERT INTO market_mk12_deal_pipeline (
            uuid, sp_id, started, piece_cid, piece_size, raw_size, offline,
            after_commp, after_psd, after_find_deal, sector, reg_seal_proof, sector_offset,
            sealed, should_index, indexing_created_at, announce
            ) VALUES (
            moved_rows.uuid, moved_rows.sp_id, TRUE, moved_rows.piece_cid, moved_rows.piece_size, moved_rows.raw_size, FALSE,
            TRUE, TRUE, TRUE, moved_rows.sector, moved_rows.reg_seal_proof, moved_rows.sector_offset,
            TRUE, TRUE, NOW() AT TIME ZONE 'UTC', moved_rows.should_announce
            ) ON CONFLICT (uuid) DO NOTHING;
        -- Remove the entry from the migration table
        DELETE FROM market_mk12_deal_pipeline_migration WHERE uuid = moved_rows.uuid;
    END LOOP;

    RAISE NOTICE 'Moved % entries to the pipeline table.', num_entries_to_move;
END;
$$;


