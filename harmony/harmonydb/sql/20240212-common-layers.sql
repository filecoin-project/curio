INSERT INTO harmony_config (title, config) VALUES
  ('post', '
  [Subsystems]
  EnableWindowPost = true
  EnableWinningPost = true
  '),

  ('gui', '
  [Subsystems]
  EnableWebGui = true
  '),

  ('seal', '
  [Subsystems]
  EnableSealSDR = true
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  EnablePoRepProof = true
  EnableSendCommitMsg = true
  EnableMoveStorage = true
  '), -- 20240904-scrub-unseal-check.sql adds EnableScrubUnsealed = true

  ('seal-gpu', '
  [Subsystems]
  EnableSealSDRTrees = true
  EnableSendPrecommitMsg = true
  '), -- 20240904-scrub-unseal-check.sql adds EnableScrubUnsealed = true
  ('seal-snark', '
  [Subsystems]
  EnablePoRepProof = true
  EnableSendCommitMsg = true
  '),
  ('sdr', '
  [Subsystems]
  EnableSealSDR = true
  '),
  
  ('storage', '
  [Subsystems]
  EnableMoveStorage = true
  ')
  ON CONFLICT (title) DO NOTHING; -- SPs may have these names defined already.