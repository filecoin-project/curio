package proof

import (
	"context"
	"io"

	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
)

// PieceReader abstracts reading raw (unpadded) piece data by CID.
// The returned reader must support both sequential reads and random access
// (io.ReaderAt) to allow section-based reads for cached proofs.
// The caller is responsible for closing the reader.
type PieceReader interface {
	GetPieceReader(ctx context.Context, pieceCid cid.Cid) (SectionReadCloser, abi.UnpaddedPieceSize, error)
}

// SectionReadCloser is a reader that supports both sequential and random access.
type SectionReadCloser interface {
	io.Reader
	io.ReaderAt
	io.Closer
}

// ProofCache abstracts the snapshot layer cache (backed by CQL in production).
// GetLayer returns the full cached layer as concatenated 32-byte node hashes
// (a flat []byte slice of length numNodes * NODE_SIZE).
type ProofCache interface {
	GetLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error)
	GetNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, [32]byte, error)
	GetLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]byte, error)
}

// CacheProofParams holds the computed positions and byte ranges for generating
// a cached split proof. All fields are derived from the snapshot layer index
// and the challenged leaf position.
type CacheProofParams struct {
	SnapshotNodeIndex int64               // which snapshot node contains the challenged leaf
	StartLeaf         int64               // first leaf covered by this snapshot node
	LeavesPerNode     int64               // number of leaves per snapshot node (2^layerIdx)
	SubTreeChallenge  int64               // challenged leaf's position within the snapshot node's subtree
	SectionOffset     int64               // unpadded byte offset into piece data for this section
	SectionLength     int64               // unpadded byte length of the section
	SubrootSize       abi.PaddedPieceSize // padded size for the sub-section memtree
}

// ComputeCacheProofParams derives the snapshot-relative positions and byte ranges
// needed to generate a cached proof for the given challenged leaf.
func ComputeCacheProofParams(layerIdx int, challengedLeaf int64) CacheProofParams {
	leavesPerNode := int64(1) << layerIdx
	snapshotNodeIndex := challengedLeaf >> layerIdx
	startLeaf := snapshotNodeIndex << layerIdx

	offset := int64(abi.PaddedPieceSize(startLeaf * NODE_SIZE).Unpadded())
	length := int64(abi.PaddedPieceSize(leavesPerNode * NODE_SIZE).Unpadded())
	subrootSize := padreader.PaddedSize(uint64(length)).Padded()

	return CacheProofParams{
		SnapshotNodeIndex: snapshotNodeIndex,
		StartLeaf:         startLeaf,
		LeavesPerNode:     leavesPerNode,
		SubTreeChallenge:  challengedLeaf - startLeaf,
		SectionOffset:     offset,
		SectionLength:     length,
		SubrootSize:       subrootSize,
	}
}

// CombineSplitProofs joins a sub-tree proof (leaf -> snapshot node) with an
// upper proof (snapshot node -> root) into a single end-to-end proof.
func CombineSplitProofs(subProof, upperProof *RawMerkleProof) *RawMerkleProof {
	return &RawMerkleProof{
		Leaf:  subProof.Leaf,
		Proof: append(subProof.Proof, upperProof.Proof...),
		Root:  upperProof.Root,
	}
}

// GenerateCachedProof builds a split merkle proof using a cached middle layer.
// It reads only the section of data around the challenged leaf (not the full piece),
// builds a small memtree for that section, then combines it with a proof from the
// cached snapshot layer up to the root.
//
// This operates on a single piece. If the caller uses subpiece aggregation
// (piece composed of multiple subpieces), it should call this per-subpiece and
// join the result with any higher-level tree proof externally.
//
// Returns (nil, nil) if no cache is available, signaling the caller to fall back
// to full memtree proof generation.
func GenerateCachedProof(
	ctx context.Context,
	reader PieceReader,
	cache ProofCache,
	pieceCidV2 cid.Cid,
	challengedLeaf int64,
) (*RawMerkleProof, error) {
	has, layerIdx, err := cache.GetLayerIndex(ctx, pieceCidV2)
	if err != nil {
		return nil, xerrors.Errorf("failed to check PDP layer: %w", err)
	}
	if !has {
		return nil, nil
	}

	params := ComputeCacheProofParams(layerIdx, challengedLeaf)

	has, nodeHash, err := cache.GetNode(ctx, pieceCidV2, layerIdx, params.SnapshotNodeIndex)
	if err != nil {
		return nil, xerrors.Errorf("failed to get cached node: %w", err)
	}
	if !has {
		return nil, nil
	}

	log.Debugw("GenerateCachedProof", "challengedLeaf", challengedLeaf, "layerIdx", layerIdx,
		"snapshotNodeIndex", params.SnapshotNodeIndex, "startLeaf", params.StartLeaf, "leavesPerNode", params.LeavesPerNode)

	// Derive v1 CID for piece reader lookup
	pieceCidV1, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		return nil, xerrors.Errorf("failed to derive v1 CID from v2: %w", err)
	}

	pieceReader, reportedSize, err := reader.GetPieceReader(ctx, pieceCidV1)
	if err != nil {
		return nil, xerrors.Errorf("failed to get reader: %w", err)
	}
	defer func() {
		_ = pieceReader.Close()
	}()

	// Read only the section covering the challenged snapshot node
	dataReader := io.NewSectionReader(pieceReader, params.SectionOffset, params.SectionLength)
	fileRemaining := int64(reportedSize) - params.SectionOffset

	var data io.Reader
	if fileRemaining < params.SectionLength {
		data = io.MultiReader(dataReader, nullreader.NewNullReader(abi.UnpaddedPieceSize(int64(params.SubrootSize.Unpadded())-fileRemaining)))
	} else {
		data = dataReader
	}

	memtree, err := BuildSha254Memtree(data, params.SubrootSize.Unpadded())
	if err != nil {
		return nil, xerrors.Errorf("failed to build section memtree: %w", err)
	}
	defer pool.Put(memtree)

	// Get proof within the small section memtree
	subTreeProof, err := MemtreeProof(memtree, params.SubTreeChallenge)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate section proof: %w", err)
	}

	// Verify the section root matches the cached node
	if subTreeProof.Root != nodeHash {
		return nil, xerrors.Errorf("section root mismatch with cached node: %x != %x", subTreeProof.Root, nodeHash)
	}

	// Fetch full cached layer and build tree from snapshot to root
	layerBytes, err := cache.GetLayer(ctx, pieceCidV2, layerIdx)
	if err != nil {
		return nil, xerrors.Errorf("failed to get cached layer nodes: %w", err)
	}
	log.Debugw("GenerateCachedProof layerBytes", "size", len(layerBytes), "numNodes", len(layerBytes)/NODE_SIZE)

	mtree, err := BuildSha254MemtreeFromSnapshot(layerBytes)
	if err != nil {
		return nil, xerrors.Errorf("failed to build memtree from snapshot: %w", err)
	}
	defer pool.Put(mtree)

	// Generate proof from snapshot node to piece root
	snapshotProof, err := MemtreeProof(mtree, params.SnapshotNodeIndex)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate snapshot-to-root proof: %w", err)
	}

	return CombineSplitProofs(subTreeProof, snapshotProof), nil
}
