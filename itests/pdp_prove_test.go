package itests

import (
	"context"
	"io"
	"math/rand"
	"os"
	"testing"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/savecache"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/pdp"

	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
)

// TestPDPProving verifies the functionality of generating and validating PDP proofs
// using a random file and a cached snapshot layer stored in Cassandra.
func TestPDPProving(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultCurioConfig()
	idxStore, err := indexstore.NewIndexStore([]string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, cfg)
	require.NoError(t, err)
	// Start the index store in test mode. This creates a temporary keyspace
	// so tests can run against a clean, isolated Cassandra instance.
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	dir := t.TempDir()

	rawSize := int64(5 * 1024 * 1024)
	pieceSize := padreader.PaddedSize(uint64(rawSize)).Padded()

	// Create a temporary random file of the desired raw size. This file
	// will be used to compute a commP and a snapshot layer for the test.
	fileStr, err := testutils.CreateRandomFile(dir, 0, rawSize)
	require.NoError(t, err)

	defer func() {
		_ = os.Remove(fileStr)
	}()

	f, err := os.Open(fileStr)
	require.NoError(t, err)

	stat, err := f.Stat()
	require.NoError(t, err)
	require.Equal(t, stat.Size(), rawSize)

	defer func() {
		_ = f.Close()
	}()

	t.Logf("File Size: %d", stat.Size())

	// Total number of leafs
	numberOfLeafs := pieceSize.Unpadded() / 32

	// Compute the piece commitment (commP) and capture a snapshot layer.
	// The snapshot layer records node digests at a middle merkle-tree level
	// which we will store in the indexstore and later use to accelerate
	// proof generation.
	cp := savecache.NewCommPWithSizeForTest(uint64(rawSize))
	_, err = io.Copy(cp, f)
	require.NoError(t, err)

	digest, psize, layerIdx, expectedNodeCount, layer, err := cp.DigestWithSnapShot()
	require.NoError(t, err)

	require.Equal(t, abi.PaddedPieceSize(psize), pieceSize)

	t.Logf("Digest: %x", digest)
	t.Logf("PieceSize: %d", psize)
	t.Logf("LayerIdx: %d", layerIdx)
	t.Logf("Expected Node Count: %d", expectedNodeCount)
	t.Logf("Number of Nodes in snapshot layer: %d", len(layer))
	t.Logf("Total Number of Leafs: %d", numberOfLeafs)

	pcid2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(stat.Size()))
	require.NoError(t, err)

	leafs := make([]indexstore.NodeDigest, len(layer))
	for i, s := range layer {
		leafs[i] = indexstore.NodeDigest{
			Layer: layerIdx,
			Hash:  s.Hash,
			Index: int64(i),
		}
	}
	require.Equal(t, len(leafs), len(layer))

	// Persist the snapshot layer into Cassandra (indexstore). This simulates
	// the caching step that `TaskPDPSaveCache` would perform in production.
	// The layer can later be fetched by `ProveTask` to avoid rebuilding the
	// full memtree for the entire sub-piece.
	err = idxStore.AddPDPLayer(ctx, pcid2, leafs)
	require.NoError(t, err)

	// Choose a random challenge leaf index inside the piece (in terms of
	// 32-byte leaves). This is the same selection logic used by the on-chain
	// PDP verifier to pick challenge positions.
	challenge := int64(rand.Intn(int(numberOfLeafs)))

	t.Logf("Challenge: %d", challenge)

	has, outLayerIndex, err := idxStore.GetPDPLayerIndex(ctx, pcid2)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, outLayerIndex, layerIdx)

	// Calculate start leaf and snapshot leaf indexes
	leavesPerNode := int64(1) << outLayerIndex
	snapshotNodeIndex := challenge >> outLayerIndex
	startLeaf := snapshotNodeIndex << outLayerIndex
	t.Logf("Leaves per Node: %d", leavesPerNode)
	t.Logf("Start Leaf: %d", startLeaf)
	t.Logf("Snapshot Node Index: %d", snapshotNodeIndex)

	has, snapNode, err := idxStore.GetPDPNode(ctx, pcid2, outLayerIndex, snapshotNodeIndex)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, snapNode.Index, snapshotNodeIndex)
	require.Equal(t, snapNode.Layer, layerIdx)
	require.Equal(t, snapNode.Hash, layer[snapshotNodeIndex].Hash)

	// Convert the tree-based leaf range covered by the snapshot node into a
	// byte-range (offset/length) within the original file. We will read only
	// this subsection and build a small memtree for it, rather than building
	// a memtree for the whole piece.
	offset := int64(abi.PaddedPieceSize(startLeaf * 32).Unpadded())
	length := int64(abi.PaddedPieceSize(leavesPerNode * 32).Unpadded())

	t.Logf("Offset: %d", offset)
	t.Logf("Length: %d", length)

	// Compute the padded size for the subsection; this is the size we'll
	// pass to `BuildSha254Memtree` to construct the sub-tree memtree.
	subrootSize := padreader.PaddedSize(uint64(length)).Padded()
	t.Logf("Subroot Size: %d", subrootSize)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	dataReader := io.NewSectionReader(f, offset, length)

	fileRemaining := stat.Size() - offset

	t.Logf("File Remaining: %d", fileRemaining)
	t.Logf("Is Padding: %t", fileRemaining < length)

	var data io.Reader
	if fileRemaining < length {
		data = io.MultiReader(dataReader, nullreader.NewNullReader(abi.UnpaddedPieceSize(int64(subrootSize.Unpadded())-fileRemaining)))
	} else {
		data = dataReader
	}

	// Build an in-memory merkle tree for the subsection covered by the
	// snapshot node. In the cached flow this corresponds to the smaller
	// memtree we build for the subroot; in production we only do this for
	// the chunk around the challenged leaf.
	memtree, err := proof.BuildSha254Memtree(data, subrootSize.Unpadded())
	require.NoError(t, err)
	defer pool.Put(memtree)

	// Get challenge leaf in subTree
	subTreeChallenge := challenge - startLeaf

	// Generate merkle proof for subTree
	subTreeProof, err := proof.MemtreeProof(memtree, subTreeChallenge)
	require.NoError(t, err)

	// Verify that subTree root is same as snapNode hash
	require.Equal(t, subTreeProof.Root, snapNode.Hash)

	// Arrange the full snapshot layer retrieved from Cassandra into a single
	// contiguous byte slice (concatenation of 32-byte hashes). We will use
	// `BuildSha254MemtreeFromSnapshot` to construct a memtree over this layer
	// and then generate a proof from the snapshot node up to the commP.
	var layerBytes []byte
	outLayer, err := idxStore.GetPDPLayer(ctx, pcid2, layerIdx)
	require.NoError(t, err)
	require.Equal(t, len(outLayer), len(leafs))
	require.Equal(t, len(outLayer), len(layer))
	require.Equal(t, outLayer, leafs)
	for _, n := range outLayer {
		layerBytes = append(layerBytes, n.Hash[:]...)
	}

	t.Logf("Layer Bytes: %d", len(layerBytes))

	// Build a memtree for the snapshot layer (this reconstructs the upper
	// levels of the piece's merkle tree, from the snapshot layer up to the
	// commP). This allows us to produce proof elements from the snapshot
	// node to the final commP root without touching the original file data.
	mtree, err := proof.BuildSha254MemtreeFromSnapshot(layerBytes)
	require.NoError(t, err)
	defer pool.Put(mtree)

	// Generate merkle proof from snapShot node to commP
	proofs, err := proof.MemtreeProof(mtree, snapshotNodeIndex)
	require.NoError(t, err)

	var digest32 [32]byte
	copy(digest32[:], digest[:])

	// Sanity check: the root computed from the snapshot layer must equal
	// the commP (digest) we computed earlier.
	require.Equal(t, proofs.Root, digest32)
	rd := proofs.Root

	out := contract.IPDPTypesProof{
		Leaf:  subTreeProof.Leaf,
		Proof: append(subTreeProof.Proof, proofs.Proof...),
	}

	// Verify the combined proof (leaf->snapshot + snapshot->commP) against
	// the expected root. This simulates the final verification step that
	// would be performed before sending a proof on-chain.
	verified := pdp.Verify(out, rd, uint64(challenge))
	require.True(t, verified)

	// Clean up the cached layer from the index store - keep tests isolated.
	err = idxStore.DeletePDPLayer(ctx, pcid2)
	require.NoError(t, err)
}
