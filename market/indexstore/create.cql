CREATE TABLE IF NOT EXISTS PayloadToPiece (
    PieceCid BLOB,
    PayloadMultihash BLOB,
    BlockOffset BIGINT,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
) WITH transactions = { 'enabled' : true };

CREATE INDEX IF NOT EXISTS idx_piece_multihash
    ON PayloadToPiece (PieceCid, PayloadMultihash);