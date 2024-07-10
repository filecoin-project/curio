CREATE TABLE harmony_cron (
    id SERIAL PRIMARY KEY NOT NULL,
    task_id INTEGER NOT NULL REFERENCES harmony_task (id) ON DELETE CASCADE,
    task_name VARCHAR(16) NOT NULL,
    sql_table TEXT NOT NULL,
    sql_row_id INTEGER NOT NULL,
);

CREATE OR REPLACE FUNCTION update_ext_taskid(table_name text, tid integer, id_value integer)
RETURNS void AS $$
BEGIN
    EXECUTE format('UPDATE %I SET task_id = $1 WHERE id=$2', table_name)
    USING tid, id_value;
END;
$$ LANGUAGE plpgsql;