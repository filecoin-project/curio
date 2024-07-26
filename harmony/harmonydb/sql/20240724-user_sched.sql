CREATE TABLE harmony_task_user (
  task_id INTEGER PRIMARY KEY, 
  owner TEXT NOT NULL, 
  expiration INTEGER NOT NULL,
  ignore_userscheduler BOOLEAN NOT NULL DEFAULT FALSE,
  FOREIGN KEY (task_id) REFERENCES harmony_task (task_id) ON DELETE CASCADE
);