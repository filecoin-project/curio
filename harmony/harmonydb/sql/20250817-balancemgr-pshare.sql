ALTER TABLE balance_manager_addresses ADD COLUMN IF NOT EXISTS subject_type TEXT NOT NULL DEFAULT 'wallet';

-- For proofshare rules we allow subject_address == second_address.
-- Relax the constraint accordingly.
ALTER TABLE balance_manager_addresses DROP CONSTRAINT IF EXISTS subject_not_equal_second;
ALTER TABLE balance_manager_addresses ADD CONSTRAINT subject_not_equal_second
  CHECK (subject_type = 'proofshare' OR subject_address != second_address);
