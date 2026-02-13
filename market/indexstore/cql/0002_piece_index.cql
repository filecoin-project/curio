CREATE TABLE IF NOT EXISTS pdp_cache_layer (
    PieceCid   blob,
    LayerIndex int,
    LeafIndex  bigint,
    Leaf       blob,
    PRIMARY KEY ((PieceCid, LayerIndex), LeafIndex)
) WITH CLUSTERING ORDER BY (LeafIndex ASC);