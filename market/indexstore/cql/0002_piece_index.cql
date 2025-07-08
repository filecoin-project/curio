CREATE TABLE IF NOT EXISTS PieceToAggregatePiece (
    PieceCid BLOB,
    AggregatePieceCid BLOB,
    UnpaddedOffset BIGINT,
    UnpaddedLength BIGINT,
    PRIMARY KEY (PieceCid, AggregatePieceCid)
);

CREATE TABLE IF NOT EXISTS PDPCacheLayer (
    PieceCid BLOB PRIMARY KEY,
    LayerIndex INT,
    Leaf BLOB,
    LeafIndex BIGINT
);