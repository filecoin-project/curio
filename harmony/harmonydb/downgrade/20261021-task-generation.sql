DROP TRIGGER harmony_task_owner_generation ON harmony_task;
DROP FUNCTION harmony_task_owner_generation();
ALTER TABLE harmony_task DROP COLUMN owner_generation;
