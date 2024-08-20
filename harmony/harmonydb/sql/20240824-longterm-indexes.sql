create index mining_base_block_task_id_id_index
    on mining_base_block (task_id, id);

create index message_waits_waiter_machine_id_index
    on message_waits (waiter_machine_id);

create index message_sends_signed_cid_index
    on message_sends (signed_cid);