ALTER TABLE balance_manager_addresses DROP CONSTRAINT IF EXISTS subject_not_equal_second;
ALTER TABLE balance_manager_addresses ADD CONSTRAINT subject_not_equal_second CHECK (subject_address != second_address);
