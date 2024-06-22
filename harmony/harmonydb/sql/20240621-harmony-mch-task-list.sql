ALTER TABLE `harmony_task` ADD COLUMN `created_time` TIMESTAMP NOT NULL DEFAULT current_timestamp;
CREATE TABLE `harmony_task_bid` (
    `id` SERIAL PRIMARY KEY NOT NULL,
    `task_id` INTEGER NOT NULL,
    `bid` FLOAT NOT NULL,
    `bidder` TIMESTAMP NOT NULL
    `update_time` TIMESTAMP NOT NULL DEFAULT current_timestamp
);