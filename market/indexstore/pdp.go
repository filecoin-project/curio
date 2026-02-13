package indexstore

import (
	"context"
	"errors"
	"sort"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/gocql"
	"golang.org/x/xerrors"
)

// ---------------------------------------------------------------------------
// PDP cache layer operations
// ---------------------------------------------------------------------------

// AddPDPLayer batch-inserts a set of Merkle-tree NodeDigest entries for a
// piece. All entries must belong to the same layer. Returns an error if
// the slice is empty.
func (i *IndexStore) AddPDPLayer(ctx context.Context, pieceCid cid.Cid, layer []NodeDigest) error {
	if len(layer) == 0 {
		return xerrors.Errorf("no records to insert")
	}

	const qry = `INSERT INTO pdp_cache_layer (PieceCid, LayerIndex, Leaf, LeafIndex) VALUES (?, ?, ?, ?)`
	pieceCidBytes := pieceCid.Bytes()
	var batch *gocql.Batch

	for _, nd := range layer {
		// Lazily create a new batch.
		if batch == nil {
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}

		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt: qry, Args: []interface{}{pieceCidBytes, nd.Layer, nd.Hash[:], nd.Index}, Idempotent: true,
		})

		// Flush when the batch reaches the configured size limit.
		if len(batch.Entries) >= i.settings.InsertBatchSize {
			if err := i.session.ExecuteBatch(batch); err != nil {
				return xerrors.Errorf("batch insert PDP layer for piece %s: %w", pieceCid, err)
			}
			batch = nil
		}
	}

	// Flush any remaining entries.
	if batch != nil && len(batch.Entries) > 0 {
		if err := i.session.ExecuteBatch(batch); err != nil {
			return xerrors.Errorf("batch insert PDP layer for piece %s: %w", pieceCid, err)
		}
	}
	return nil
}

// GetPDPLayerIndex returns the layer index of the first cached PDP layer
// for the piece, or (false, 0) if none exists.
func (i *IndexStore) GetPDPLayerIndex(ctx context.Context, pieceCid cid.Cid) (bool, int, error) {
	var layerIdx int
	err := i.session.Query(
		`SELECT LayerIndex FROM pdp_cache_layer WHERE PieceCid = ? LIMIT 1`, pieceCid.Bytes(),
	).WithContext(ctx).Scan(&layerIdx)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return false, 0, nil
		}
		return false, 0, xerrors.Errorf("querying PDP layer index for piece %s: %w", pieceCid, err)
	}
	return true, layerIdx, nil
}

// GetPDPLayer returns all cached nodes for the given piece and layer,
// sorted by ascending node index.
func (i *IndexStore) GetPDPLayer(ctx context.Context, pieceCid cid.Cid, layerIdx int) ([]NodeDigest, error) {
	iter := i.session.Query(
		`SELECT LeafIndex, Leaf FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ?`,
		pieceCid.Bytes(), layerIdx,
	).WithContext(ctx).PageSize(pdpPageSize).Iter()

	var layer []NodeDigest
	var leafIdx int64
	var leaf []byte
	for iter.Scan(&leafIdx, &leaf) {
		layer = append(layer, NodeDigest{Layer: layerIdx, Index: leafIdx, Hash: [32]byte(leaf)})
		// Allocate a fresh buffer so the next Scan doesn't overwrite this entry.
		leaf = make([]byte, hashDigestSize)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("reading PDP layer for piece %s: %w", pieceCid, err)
	}

	// Cassandra may not return rows in LeafIndex order – sort explicitly.
	sort.Slice(layer, func(a, b int) bool { return layer[a].Index < layer[b].Index })
	return layer, nil
}

// DeletePDPLayer removes all cached PDP layers for the given piece.
// It loops because the table is partitioned by (PieceCid, LayerIndex) and
// each layer must be deleted individually.
func (i *IndexStore) DeletePDPLayer(ctx context.Context, pieceCid cid.Cid) error {
	for {
		has, layerIdx, err := i.GetPDPLayerIndex(ctx, pieceCid)
		if err != nil {
			return err
		}
		// No more layers remain – we're done.
		if !has {
			return nil
		}
		qry := `DELETE FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ?`
		if err := i.session.Query(qry, pieceCid.Bytes(), layerIdx).WithContext(ctx).Exec(); err != nil {
			return xerrors.Errorf("deleting PDP layer %d for piece %s: %w", layerIdx, pieceCid, err)
		}
	}
}

// GetPDPNode retrieves a single cached Merkle-tree node.
// Returns (false, nil, nil) when the node does not exist.
func (i *IndexStore) GetPDPNode(ctx context.Context, pieceCid cid.Cid, layerIdx int, index int64) (bool, *NodeDigest, error) {
	var r []byte
	qry := `SELECT Leaf FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ? AND LeafIndex = ? LIMIT 1`
	err := i.session.Query(qry, pieceCid.Bytes(), layerIdx, index).WithContext(ctx).Scan(&r)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return false, nil, nil
		}
		return false, nil, xerrors.Errorf("querying PDP node for piece %s: %w", pieceCid, err)
	}

	var hash [hashDigestSize]byte
	copy(hash[:], r)
	return true, &NodeDigest{Layer: layerIdx, Index: index, Hash: hash}, nil
}
