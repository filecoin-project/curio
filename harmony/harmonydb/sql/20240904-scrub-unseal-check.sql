CREATE TABLE IF NOT EXISTS scrub_unseal_commd_check (
    check_id BIGSERIAL PRIMARY KEY,

    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,
    create_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,

    task_id BIGINT,

    expected_unsealed_cid TEXT NOT NULL,

    -- results
    ok BOOLEAN,
    actual_unsealed_cid TEXT,
    message TEXT,

    UNIQUE (sp_id, sector_number, create_time),
    UNIQUE (task_id)
);

-- Add EnableScrubUnsealed to seal and seal-gpu IF they are the defaults
UPDATE harmony_config
SET config = '
  [Subsystems]
  EnableSealSDR = true
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  EnablePoRepProof = true
  EnableSendCommitMsg = true
  EnableMoveStorage = true
  EnableScrubUnsealed = true
  ' WHERE title = 'seal' AND config = '
  [Subsystems]
  EnableSealSDR = true
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  EnablePoRepProof = true
  EnableSendCommitMsg = true
  EnableMoveStorage = true
  ';

UPDATE harmony_config
SET config = '
  [Subsystems]
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  EnableScrubUnsealed = true
  ' WHERE title = 'seal-gpu' AND config = '
  [Subsystems]
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  ';