package pdp

import (
	"context"
	"io"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
)

// genSubPieceMemtreeWithCache generates a subpiece merkle tree, using cache if available
// Checks cache first (< 100ms on hit), falls back to building on-demand, and schedules background caching
func (p *ProveTask) genSubPieceMemtreeWithCache(ctx context.Context, subPieceCid string, subPieceSize abi.PaddedPieceSize) ([]byte, error) {
	// 1. Try to load from cache first
	var treeData []byte
	var status string

	err := p.db.QueryRow(ctx, `
		SELECT tree_data, status FROM pdp_piece_memtrees 
		WHERE piece_cid = $1 AND piece_padded_size = $2
	`, subPieceCid, uint64(subPieceSize)).Scan(&treeData, &status)

	if err == nil && status == "completed" && treeData != nil {
		// Cache hit! Return pre-computed tree
		logCache.Debugw("memtree cache hit", "piece_cid", subPieceCid, "size", subPieceSize)
		return treeData, nil
	}

	// 2. Cache miss or not completed yet - build the tree on-demand
	logCache.Debugw("memtree cache miss, building on-demand", "piece_cid", subPieceCid, "size", subPieceSize)

	// Build using the original method
	tree, err := p.genSubPieceMemtree(ctx, subPieceCid, subPieceSize)
	if err != nil {
		return nil, err
	}

	// 3. Opportunistically schedule caching in background (non-blocking)
	go func() {
		if err := ScheduleCacheTaskForPiece(context.Background(), p.db, subPieceCid, int64(subPieceSize)); err != nil {
			logCache.Warnw("failed to schedule cache task", "piece_cid", subPieceCid, "error", err)
		}
	}()

	return tree, nil
}