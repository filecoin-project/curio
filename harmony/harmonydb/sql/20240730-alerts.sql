CREATE TABLE alerts (
  id SERIAL PRIMARY KEY,
  machine_name VARCHAR(255) NOT NULL,
  message TEXT NOT NULL
);