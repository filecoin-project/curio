ALTER TABLE harmony_machines ADD COLUMN IF NOT EXISTS unschedulable BOOLEAN DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_harmony_machines_unschedulable ON harmony_machines (unschedulable);
