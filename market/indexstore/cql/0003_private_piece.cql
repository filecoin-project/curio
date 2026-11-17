CREATE TABLE IF NOT EXISTS PrivatePayloadToPiece (
    PayloadMultihash BLOB,
    PieceCid BLOB,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
);

CREATE TABLE IF NOT EXISTS PrivatePieceBlock (
    PieceCid BLOB,
    PayloadMultihash BLOB,
    BlockOffset BIGINT,
    PRIMARY KEY (PieceCid, PayloadMultihash)
);
