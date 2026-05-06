-- Indexes to reduce load from SealPoller full-table scans and batch CTEs.

CREATE INDEX IF NOT EXISTS idx_sdr_pipeline_active
    ON sectors_sdr_pipeline (sp_id, sector_number)
    WHERE failed = FALSE
      AND (after_commit_msg_success = FALSE OR after_move_storage = FALSE);

CREATE INDEX IF NOT EXISTS idx_sdr_pipeline_precommit_ready
    ON sectors_sdr_pipeline (sp_id, reg_seal_proof)
    WHERE after_synth = TRUE
      AND task_id_precommit_msg IS NULL
      AND after_precommit_msg = FALSE;

CREATE INDEX IF NOT EXISTS idx_sdr_pipeline_commit_ready
    ON sectors_sdr_pipeline (sp_id, reg_seal_proof, start_epoch)
    WHERE after_porep = TRUE
      AND porep_proof IS NOT NULL
      AND task_id_commit_msg IS NULL
      AND after_commit_msg = FALSE
      AND start_epoch IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_parked_pieces_pending
    ON parked_pieces (long_term)
    WHERE complete = FALSE AND task_id IS NULL AND skip = FALSE;
