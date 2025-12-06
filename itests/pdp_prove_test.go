package itests

import (
	"context"
	"io"
	"math/rand"
	"os"
	"testing"

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

// TestPDPProving verifies the functionality of generating and validating PDP proofs with a random file created in a temporary directory.
func TestPDPProving(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := config.DefaultCurioConfig()
	idxStore := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, cfg)
	err := idxStore.Start(ctx, true)
	require.NoError(t, err)

	dir := t.TempDir()

	//rawSize := int64(8323072)
	//rawSize := int64(7 * 1024 * 1024 * 1024)
	rawSize := int64(5 * 1024 * 1024)
	pieceSize := padreader.PaddedSize(uint64(rawSize)).Padded()

	// Create temporary file
	fileStr, err := testutils.CreateRandomTmpFile(dir, rawSize)
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

	// Do commP and save the snapshot layer
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

	err = idxStore.AddPDPLayer(ctx, pcid2, leafs)
	require.NoError(t, err)

	// Generate challenge leaf
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

	// Convert tree-based leaf range to file-based offset/length
	offset := int64(abi.PaddedPieceSize(startLeaf * 32).Unpadded())
	length := int64(abi.PaddedPieceSize(leavesPerNode * 32).Unpadded())

	t.Logf("Offset: %d", offset)
	t.Logf("Length: %d", length)

	// Compute padded size to build Merkle tree
	subrootSize := padreader.PaddedSize(uint64(length)).Padded()
	t.Logf("Subroot Size: %d", subrootSize)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	dataReader := io.NewSectionReader(f, offset, length)

	_, err = f.Seek(offset, io.SeekStart)
	require.NoError(t, err)

	fileRemaining := stat.Size() - offset

	t.Logf("File Remaining: %d", fileRemaining)
	t.Logf("Is Padding: %t", fileRemaining < length)

	var data io.Reader
	if fileRemaining < length {
		data = io.MultiReader(dataReader, nullreader.NewNullReader(abi.UnpaddedPieceSize(int64(subrootSize.Unpadded())-fileRemaining)))
	} else {
		data = dataReader
	}

	memtree, err := proof.BuildSha254Memtree(data, subrootSize.Unpadded())
	require.NoError(t, err)

	// Get challenge leaf in subTree
	subTreeChallenge := challenge - startLeaf

	// Generate merkle proof for subTree
	subTreeProof, err := proof.MemtreeProof(memtree, subTreeChallenge)
	require.NoError(t, err)

	// Verify that subTree root is same as snapNode hash
	require.Equal(t, subTreeProof.Root, snapNode.Hash)

	// Arrange snapshot layer into a byte array
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

	// Create subTree from snapshot to commP (root)
	mtree, err := proof.BuildSha254MemtreeFromSnapshot(layerBytes)
	require.NoError(t, err)

	// Generate merkle proof from snapShot node to commP
	proofs, err := proof.MemtreeProof(mtree, snapshotNodeIndex)
	require.NoError(t, err)

	var digest32 [32]byte
	copy(digest32[:], digest[:])

	// verify that root and commP match
	require.Equal(t, proofs.Root, digest32)
	rd := proofs.Root

	out := contract.IPDPTypesProof{
		Leaf:  subTreeProof.Leaf,
		Proof: append(subTreeProof.Proof, proofs.Proof...),
	}

	verified := pdp.Verify(out, rd, uint64(challenge))
	require.True(t, verified)

	err = idxStore.DeletePDPLayer(ctx, pcid2)
	require.NoError(t, err)
}
