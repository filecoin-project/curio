CREATE TABLE IF NOT EXISTS PieceToAggregatePiece (
    PieceCid BLOB,
    AggregatePieceCid BLOB,
    UnpaddedOffset BIGINT,
    UnpaddedLength BIGINT,
    PRIMARY KEY (PieceCid, AggregatePieceCid)
);