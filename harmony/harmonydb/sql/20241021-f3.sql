CREATE TABLE f3_tasks (
    sp_id BIGINT PRIMARY KEY,
    task_id BIGINT UNIQUE,
    previous_ticket BYTEA,

    FOREIGN KEY (task_id) REFERENCES harmony_task (id) ON DELETE SET NULL
);
