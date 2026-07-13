-- Speed up snap sector allocation by scanning eligible CC sectors for a miner in expiration order.
CREATE INDEX IF NOT EXISTS sectors_meta_snap_alloc_candidates_idx
    ON sectors_meta (sp_id, expiration_epoch, deadline, sector_num, reg_seal_proof)
    WHERE is_cc = TRUE
      AND is_live = TRUE
      AND is_faulty = FALSE
      AND expiration_epoch IS NOT NULL
      AND deadline IS NOT NULL;

-- Speed up open-sector scans used by storage ingest seal/snap allocation.
CREATE INDEX IF NOT EXISTS open_sector_pieces_spid_snap_piece_idx
    ON open_sector_pieces (sp_id, is_snap, piece_index DESC, sector_number);
