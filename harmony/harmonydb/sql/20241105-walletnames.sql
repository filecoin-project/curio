CREATE TABLE wallet_names (
    id SERIAL PRIMARY KEY,
    wallet_id VARCHAR NOT NULL,
    name VARCHAR(60) NOT NULL
);
