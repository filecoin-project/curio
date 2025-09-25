create index if not exists mining_base_block_task_id_id_index
    on mining_base_block (task_id, id);

create index if not exists message_waits_waiter_machine_id_index
    on message_waits (waiter_machine_id);

create index if not exists message_sends_signed_cid_index
    on message_sends (signed_cid);