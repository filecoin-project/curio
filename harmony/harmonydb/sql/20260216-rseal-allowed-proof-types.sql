
-- Add allowed_proof_types to rseal_delegated_partners so the provider can
-- restrict which seal proof types each partner is allowed to submit.
-- Empty array means "all proofs accepted" (backward-compatible default).
ALTER TABLE rseal_delegated_partners ADD COLUMN IF NOT EXISTS allowed_proof_types bigint[] NOT NULL DEFAULT '{}';
