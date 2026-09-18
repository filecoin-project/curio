create table if not exists harmony_task_singleton_disabled (
    task_name varchar(255) not null primary key,
    disabled_at timestamptz not null default current_timestamp,
    reason text
);
