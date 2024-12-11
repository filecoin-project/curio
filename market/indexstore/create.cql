CREATE TABLE IF NOT EXISTS PayloadToPieces (
    PayloadMultihash BLOB,
    PieceCid BLOB,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
);

CREATE TABLE IF NOT EXISTS PieceBlockOffsetSize (
    PieceCid BLOB,
    PayloadMultihash BLOB,
    BlockOffset BIGINT,
    PRIMARY KEY (PieceCid, PayloadMultihash)
);
