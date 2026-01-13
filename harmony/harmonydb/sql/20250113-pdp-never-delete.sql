-- Add roots_added flag to track processing status
-- This aligns pdp_proofset_root_adds with pdp_proofset_creates behavior (never delete, only mark as processed)
ALTER TABLE pdp_proofset_root_adds ADD COLUMN IF NOT EXISTS roots_added BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_pdp_proofset_root_adds_roots_added ON pdp_proofset_root_adds(roots_added);