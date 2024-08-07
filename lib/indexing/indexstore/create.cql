CREATE TABLE IF NOT EXISTS PayloadToPiece (
    PieceCid BLOB,
    PayloadMultihash BLOB,
    BlockOffset BIGINT,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
);

CREATE INDEX piece_to_payload ON PayloadToPiece(PieceCid, PayloadMultihash);