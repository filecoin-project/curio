CREATE TABLE paramfetch_urls (
    machine INT,
    cid TEXT,

    PRIMARY KEY (machine, cid),
    FOREIGN KEY (machine) REFERENCES harmony_machines (id) ON DELETE CASCADE
);
