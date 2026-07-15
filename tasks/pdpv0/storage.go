package pdpv0

import (
	"context"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

// This file defines the storage seams the PDP tasks depend on, so that a
// consumer can supply its own implementations instead of being bound to Curio's
// concrete storage stack. The concrete Curio types satisfy these interfaces
// as-is, so Curio's wiring is unchanged:
//
//	*cachedreader.CachedPieceReader -> PieceReader
//	*indexstore.IndexStore          -> ProofCacheStore, IndexCleaner

// PieceReader provides random-access reads of raw piece bytes by piece CID.
// Satisfied by *cachedreader.CachedPieceReader.
type PieceReader interface {
	GetSharedPieceReader(ctx context.Context, pieceCid cid.Cid, retrieval bool) (storiface.Reader, uint64, error)
}

// ProofCacheStore reads and writes the PDP Merkle-proof layer cache used by the
// prove and save-cache tasks. Satisfied by *indexstore.IndexStore.
type ProofCacheStore interface {
	GetPDPLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error)
	GetPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]indexstore.NodeDigest, error)
	GetPDPNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, *indexstore.NodeDigest, error)
	AddPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layer []indexstore.NodeDigest) error
}

// IndexCleaner removes a piece's retrieval/IPNI indexes. Satisfied by
// *indexstore.IndexStore.
type IndexCleaner interface {
	RemoveIndexes(ctx context.Context, pieceCidv2 cid.Cid) error
}
