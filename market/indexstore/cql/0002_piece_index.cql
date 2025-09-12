CREATE TABLE IF NOT EXISTS aggregate_by_piece (
    PieceCid          blob,
    AggregatePieceCid blob,
    UnpaddedOffset    bigint,
    UnpaddedLength    bigint,
    PRIMARY KEY ((PieceCid), AggregatePieceCid, UnpaddedOffset)
) WITH CLUSTERING ORDER BY (AggregatePieceCid ASC, UnpaddedOffset ASC);

CREATE TABLE IF NOT EXISTS piece_by_aggregate (
    AggregatePieceCid blob,
    PieceCid          blob,
    UnpaddedOffset    bigint,
    UnpaddedLength    bigint,
    PRIMARY KEY ((AggregatePieceCid), PieceCid, UnpaddedOffset)
) WITH CLUSTERING ORDER BY (PieceCid ASC, UnpaddedOffset ASC);

CREATE TABLE IF NOT EXISTS pdp_cache_layer (
    PieceCid   blob,
    LayerIndex int,
    LeafIndex  bigint,
    Leaf       blob,
    PRIMARY KEY ((PieceCid, LayerIndex), LeafIndex)
) WITH CLUSTERING ORDER BY (LeafIndex ASC);