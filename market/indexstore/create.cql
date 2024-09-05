CREATE TABLE IF NOT EXISTS PayloadToPiece (
    PieceCid BLOB, -- 20 bytes trimmed
    PayloadMultihash BLOB,
    BlockOffset BIGINT,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
);