CREATE TABLE IF NOT EXISTS harmony_test (
    task_id bigint         
        constraint harmony_test_pk
            primary key,
    options text,
    result text
);
ALTER TABLE wdpost_proofs ADD COLUMN IF NOT EXISTS test_task_id bigint;