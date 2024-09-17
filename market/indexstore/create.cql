CREATE TABLE IF NOT EXISTS PayloadToPiece (
    PieceCid BLOB,
    PayloadMultihash BLOB, -- 20 bytes trimmed
    BlockOffset BIGINT,
    BlockSize BIGINT,
    PRIMARY KEY (PayloadMultihash, PieceCid)
) WITH transactions = { 'enabled' : true };

CREATE INDEX IF NOT EXISTS idx_piece_multihash
    ON PayloadToPiece (PieceCid, PayloadMultihash);