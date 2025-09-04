CREATE TABLE IF NOT EXISTS PieceToAggregatePiece (
    AggregatePieceCid BLOB PRIMARY KEY,
    PieceCid BLOB,
    UnpaddedOffset BIGINT,
    UnpaddedLength BIGINT
);

CREATE TABLE IF NOT EXISTS PDPCacheLayer (
    PieceCid BLOB PRIMARY KEY,
    LayerIndex INT,
    Leaf BLOB,
    LeafIndex BIGINT
);