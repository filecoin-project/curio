ALTER TABLE harmony_machines
ADD COLUMN unschedulable BOOLEAN DEFAULT FALSE;

CREATE INDEX idx_harmony_machines_unschedulable ON harmony_machines (unschedulable);
