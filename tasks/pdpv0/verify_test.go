// verify_test.go tests the Verify() function from task_prove.go and the
// supporting tree-construction machinery it depends on.
//
// Context (https://github.com/filecoin-project/curio/issues/888):
//
//   The Verify() function checks a Merkle inclusion proof against a known
//   piece-commitment (commP) root. It is critical to PDP (Provable Data
//   Possession) because a faulty implementation could accept invalid proofs
//   or reject valid ones.
//
//   savecache.go (lib/savecache/savecache.go) is a modified copy of the
//   upstream go-fil-commp-hashhash library, with the addition of a snapshot
//   layer cache (hashSlab254). Because it is a copy-paste, we must verify
//   that it produces identical roots to the canonical implementation.
//
// Test strategy:
//
//   1. Manual tree tests — build tiny (2/4/8-leaf) trees by hand, construct
//      every possible proof manually, and verify. These are the "ground
//      truth" tests that depend on nothing but SHA-256.
//
//   2. Memtree roundtrip tests — use BuildSha254Memtree + MemtreeProof to
//      build trees of various sizes and verify at interesting challenge
//      positions (boundaries, midpoints, etc.).
//
//   3. commp-hashhash fixture tests — feed the same deterministic random data
//      used by go-fil-commp-hashhash into our memtree builder, assert roots
//      match the published commP CIDs, then generate and verify proofs.
//
//   4. savecache cross-validation tests — confirm savecache.Calc (the
//      copy-paste of commp-hashhash) produces roots identical to our memtree
//      builder AND to the upstream test vectors.
//
//   5. Negative tests — tamper with each component (leaf, sibling, root,
//      position, proof length) and confirm Verify() rejects them.
//
//   6. Data-pattern tests — exercise zero-fill and constant-byte payloads,
//      matching go-fil-commp-hashhash's zero.txt and 0xCC.txt test suites.
//
//   7. Structural tests — validate proof depth = log2(numLeaves) and the
//      nextPowerOfTwo helper.
//
// Helpers, fixtures, and data generators live in verify_test_helpers_test.go.

package pdpv0

import (
	"bytes"
	"fmt"
	"testing"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/savecache"
	"github.com/filecoin-project/curio/pdp/contract"
)

// =========================================================================
// Section 1: Manual tree construction tests
//
// These tests build Merkle trees entirely by hand using sha254() so they
// do not depend on BuildSha254Memtree or MemtreeProof. They serve as an
// independent baseline: if these pass, we know Verify() itself is correct
// for the SHA-254 binary Merkle scheme.
// =========================================================================

// TestVerify_TwoLeafTree builds the simplest possible tree (one hash) and
// verifies both leaf positions.
//
//	    root
//	   /    \
//	 L0      L1
func TestVerify_TwoLeafTree(t *testing.T) {
	leaves := makeLeaves(2)
	root := sha254(append(leaves[0][:], leaves[1][:]...))

	// Position 0: leaf is on the LEFT, sibling (L1) is appended on the right.
	require.True(t, Verify(contract.IPDPTypesProof{
		Leaf:  leaves[0],
		Proof: [][32]byte{leaves[1]},
	}, root, 0), "proof for position 0 should verify")

	// Position 1: leaf is on the RIGHT, sibling (L0) is prepended on the left.
	require.True(t, Verify(contract.IPDPTypesProof{
		Leaf:  leaves[1],
		Proof: [][32]byte{leaves[0]},
	}, root, 1), "proof for position 1 should verify")
}

// TestVerify_FourLeafTree builds a 4-leaf tree and checks all positions.
//
//	        root
//	      /      \
//	    n01      n23
//	   /   \    /   \
//	  L0   L1  L2   L3
func TestVerify_FourLeafTree(t *testing.T) {
	leaves := makeLeaves(4)

	n01 := sha254(append(leaves[0][:], leaves[1][:]...))
	n23 := sha254(append(leaves[2][:], leaves[3][:]...))
	root := sha254(append(n01[:], n23[:]...))

	tests := []struct {
		position uint64
		leaf     [32]byte
		path     [][32]byte
	}{
		{0, leaves[0], [][32]byte{leaves[1], n23}},
		{1, leaves[1], [][32]byte{leaves[0], n23}},
		{2, leaves[2], [][32]byte{leaves[3], n01}},
		{3, leaves[3], [][32]byte{leaves[2], n01}},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("pos_%d", tc.position), func(t *testing.T) {
			require.True(t, Verify(contract.IPDPTypesProof{
				Leaf:  tc.leaf,
				Proof: tc.path,
			}, root, tc.position))
		})
	}
}

// TestVerify_EightLeafTree builds an 8-leaf tree (3 levels) and checks all
// positions. This is the smallest tree that exercises every left/right
// combination at three different levels.
//
//	              root
//	          /          \
//	      n0123          n4567
//	      /    \        /    \
//	    n01    n23    n45    n67
//	   / \    / \    / \    / \
//	  L0 L1  L2 L3  L4 L5  L6 L7
func TestVerify_EightLeafTree(t *testing.T) {
	leaves := makeLeaves(8)

	// Level 1: pair siblings
	n01 := sha254(append(leaves[0][:], leaves[1][:]...))
	n23 := sha254(append(leaves[2][:], leaves[3][:]...))
	n45 := sha254(append(leaves[4][:], leaves[5][:]...))
	n67 := sha254(append(leaves[6][:], leaves[7][:]...))

	// Level 2: pair level-1 nodes
	n0123 := sha254(append(n01[:], n23[:]...))
	n4567 := sha254(append(n45[:], n67[:]...))

	// Root
	root := sha254(append(n0123[:], n4567[:]...))

	tests := []struct {
		pos  uint64
		leaf [32]byte
		path [][32]byte
	}{
		{0, leaves[0], [][32]byte{leaves[1], n23, n4567}},
		{1, leaves[1], [][32]byte{leaves[0], n23, n4567}},
		{2, leaves[2], [][32]byte{leaves[3], n01, n4567}},
		{3, leaves[3], [][32]byte{leaves[2], n01, n4567}},
		{4, leaves[4], [][32]byte{leaves[5], n67, n0123}},
		{5, leaves[5], [][32]byte{leaves[4], n67, n0123}},
		{6, leaves[6], [][32]byte{leaves[7], n45, n0123}},
		{7, leaves[7], [][32]byte{leaves[6], n45, n0123}},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("pos_%d", tc.pos), func(t *testing.T) {
			require.True(t, Verify(contract.IPDPTypesProof{
				Leaf:  tc.leaf,
				Proof: tc.path,
			}, root, tc.pos))
		})
	}
}

// =========================================================================
// Section 2: Memtree roundtrip tests
//
// These tests use BuildSha254Memtree (FR32-padded in-memory tree builder)
// and MemtreeProof (proof extractor) to generate trees from deterministic
// random data (jbenet/go-random, seed 1337), then verify proofs at
// interesting positions via Verify().
//
// The sizes span from 4 leaves to 524288 leaves, covering the full range
// up to proof.MaxMemtreeSize.
// =========================================================================

// TestVerify_MemtreeProofRoundtrip builds trees at a wide range of sizes and
// verifies proofs at several interesting challenge positions per tree.
func TestVerify_MemtreeProofRoundtrip(t *testing.T) {
	for _, size := range wideUnpaddedSizes {
		nLeaves := int64(size.Padded()) / proof.NODE_SIZE

		t.Run(fmt.Sprintf("unpadded_%d_leaves_%d", size, nLeaves), func(t *testing.T) {
			rawData := generateRandomData(int64(size))

			memtree, root := buildMemtree(t, rawData, size)
			defer pool.Put(memtree)

			verifyProofsAtPositions(t, memtree, root, nLeaves,
				fmt.Sprintf("size=%d", size))
		})
	}
}

// TestVerify_AllPositions_SmallTree exhaustively verifies every leaf position
// for small trees where doing so is fast (up to 32 leaves).
func TestVerify_AllPositions_SmallTree(t *testing.T) {
	for _, size := range smallUnpaddedSizes {
		nLeaves := int64(size.Padded()) / proof.NODE_SIZE

		t.Run(fmt.Sprintf("unpadded_%d", size), func(t *testing.T) {
			rawData := generateRandomData(int64(size))

			memtree, root := buildMemtree(t, rawData, size)
			defer pool.Put(memtree)

			for pos := int64(0); pos < nLeaves; pos++ {
				p := extractProof(t, memtree, pos)
				require.True(t, Verify(p, root, uint64(pos)),
					"Verify failed size=%d pos=%d", size, pos)
			}
		})
	}
}

// =========================================================================
// Section 3: commp-hashhash fixture tests
//
// These tests use the known-good commP test vectors published in:
//   https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
//
// The data for each vector is generated by generateRandomData (seed 1337),
// which replicates the jbenet/go-random algorithm used by the upstream test
// suite. This means the roots we compute MUST match the CIDs in the fixture
// file — giving us confidence that our FR32 padding + SHA-254 hashing is
// correct.
//
// After verifying the root, we generate and verify Merkle proofs at
// interesting positions, completing the end-to-end link between
// "known-good commP" and "Verify() accepts correct proofs for that root".
// =========================================================================

// runCommPVectorTests is the shared logic for both small and larger vector
// test functions.
func runCommPVectorTests(t *testing.T, vectorData string) {
	t.Helper()

	vectors, err := parseTestVectors(vectorData)
	require.NoError(t, err)

	for _, vec := range vectors {
		if vec.paddedSize > uint64(proof.MaxMemtreeSize) {
			continue
		}

		t.Run(fmt.Sprintf("payload_%d_padded_%d", vec.payloadSize, vec.paddedSize), func(t *testing.T) {
			rawData := generateRandomData(vec.payloadSize)

			memtree, root := buildMemtreeFromPayload(t, rawData, vec.paddedSize)
			defer pool.Put(memtree)

			// Assert root matches the expected commP from the upstream fixture.
			require.Equal(t, vec.rawCommP, root,
				"commP root mismatch for payload=%d padded=%d", vec.payloadSize, vec.paddedSize)

			// Generate and verify proofs at interesting positions.
			nLeaves := int64(vec.paddedSize) / proof.NODE_SIZE
			verifyProofsAtPositions(t, memtree, root, nLeaves,
				fmt.Sprintf("payload=%d", vec.payloadSize))
		})
	}
}

// TestVerify_CommPHashHashVectors_Small runs the smallest test vectors
// (payload ≤ 2032 bytes). Fast enough for every `go test` invocation.
func TestVerify_CommPHashHashVectors_Small(t *testing.T) {
	runCommPVectorTests(t, smallRandomVectors)
}

// TestVerify_CommPHashHashVectors_Larger runs vectors up to 131072 bytes.
// Skipped by `go test -short`.
func TestVerify_CommPHashHashVectors_Larger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping larger vectors in short mode")
	}
	runCommPVectorTests(t, largerRandomVectors)
}

// =========================================================================
// Section 4: savecache cross-validation tests
//
// savecache.go (lib/savecache/savecache.go) is a copy-paste of
// go-fil-commp-hashhash/commp.go with the addition of a snapshot-layer
// cache (hashSlab254). These tests confirm that the copy produces roots
// identical to both BuildSha254Memtree and the upstream commP CIDs.
//
// ref: https://github.com/filecoin-project/curio/blob/main/lib/savecache/savecache.go#L20-L21
// =========================================================================

// TestVerify_SavecacheCalcVsMemtree compares savecache.Calc (streaming commP)
// against BuildSha254Memtree (in-memory) AND the upstream test vectors. All
// three must agree on the root, and proofs generated from the memtree must
// pass Verify().
func TestVerify_SavecacheCalcVsMemtree(t *testing.T) {
	vectors, err := parseTestVectors(smallRandomVectors)
	require.NoError(t, err)

	for _, vec := range vectors {
		if vec.paddedSize > uint64(proof.MaxMemtreeSize) {
			continue
		}

		t.Run(fmt.Sprintf("payload_%d_padded_%d", vec.payloadSize, vec.paddedSize), func(t *testing.T) {
			rawData := generateRandomData(vec.payloadSize)

			// --- Path A: savecache.Calc (streaming, copy-paste of commp-hashhash)
			savecacheRoot := computeSavecacheRoot(t, rawData)

			// --- Path B: BuildSha254Memtree (in-memory FR32 tree)
			memtree, memtreeRoot := buildMemtreeFromPayload(t, rawData, vec.paddedSize)
			defer pool.Put(memtree)

			// All three roots must agree.
			require.Equal(t, savecacheRoot, memtreeRoot,
				"savecache vs memtree root mismatch for payload=%d", vec.payloadSize)
			require.Equal(t, vec.rawCommP, memtreeRoot,
				"memtree root vs expected commP mismatch for payload=%d", vec.payloadSize)

			// Proofs generated from the memtree must pass Verify().
			nLeaves := int64(vec.paddedSize) / proof.NODE_SIZE
			verifyProofsAtPositions(t, memtree, memtreeRoot, nLeaves,
				fmt.Sprintf("payload=%d", vec.payloadSize))
		})
	}
}

// computeSavecacheRoot computes a commP root using savecache.Calc.
func computeSavecacheRoot(t *testing.T, rawData []byte) [32]byte {
	t.Helper()

	calc := &savecache.Calc{}
	_, err := calc.Write(rawData)
	require.NoError(t, err)

	var root [32]byte
	copy(root[:], calc.Sum(nil))
	return root
}

// TestVerify_SavecacheSnapshot_ProofConsistency exercises the snapshot-layer
// cache path (DigestWithSnapShot) introduced by savecache.go's hashSlab254.
// It confirms that:
//   - The snapshot root matches the full memtree root.
//   - Proofs generated from the full memtree verify against that root.
//
// This is the key test for the savecache.go-specific modification: the
// snapshot layer is the "only gap" between savecache.go and upstream
// commp-hashhash (ref: https://github.com/filecoin-project/curio/pull/997).
func TestVerify_SavecacheSnapshot_ProofConsistency(t *testing.T) {
	snapshotTestSizes := []uint64{
		2032,  // small: snapshot should capture 1 node
		4064,  // medium: snapshot should capture 2 nodes
		16256, // larger: snapshot should capture 8 nodes
	}

	for _, rawSize := range snapshotTestSizes {
		t.Run(fmt.Sprintf("size_%d", rawSize), func(t *testing.T) {
			rawData := generateRandomData(int64(rawSize))

			// --- Snapshot path: savecache with DigestWithSnapShot
			calc := savecache.NewCommPWithSizeForTest(rawSize)
			_, err := calc.Write(rawData)
			require.NoError(t, err)

			commP, paddedSize, layerIdx, expectedNodeCount, _, err := calc.DigestWithSnapShot()
			require.NoError(t, err)

			var savecacheRoot [32]byte
			copy(savecacheRoot[:], commP)

			t.Logf("size=%d paddedSize=%d layerIdx=%d expectedNodeCount=%d",
				rawSize, paddedSize, layerIdx, expectedNodeCount)

			// --- Full memtree path
			memtree, memtreeRoot := buildMemtreeFromPayload(t, rawData, paddedSize)
			defer pool.Put(memtree)

			// Roots must agree.
			require.Equal(t, savecacheRoot, memtreeRoot,
				"savecache snapshot root vs memtree root mismatch")

			// Proofs from the full memtree must pass Verify().
			nLeaves := int64(paddedSize) / proof.NODE_SIZE
			verifyProofsAtPositions(t, memtree, memtreeRoot, nLeaves,
				fmt.Sprintf("size=%d", rawSize))
		})
	}
}

// =========================================================================
// Section 5: Negative tests
//
// Each test starts with a valid proof and mutates exactly one component,
// asserting that Verify() rejects the tampered proof. This ensures the
// function is actually checking every part of the proof structure.
// =========================================================================

// TestVerify_Negative_WrongRoot flips a bit in the root — the reconstructed
// root from the valid proof will not match.
func TestVerify_Negative_WrongRoot(t *testing.T) {
	rawData := generateRandomData(127)
	p, root := buildProofFromMemtree(t, rawData, 127, 0)

	wrongRoot := root
	wrongRoot[0] ^= 0xFF

	require.False(t, Verify(p, wrongRoot, 0), "Verify should fail with wrong root")
}

// TestVerify_Negative_WrongPosition uses a valid proof at the wrong leaf
// index, which inverts the left/right hash ordering.
func TestVerify_Negative_WrongPosition(t *testing.T) {
	rawData := generateRandomData(127)
	p, root := buildProofFromMemtree(t, rawData, 127, 0)

	require.False(t, Verify(p, root, 1), "Verify should fail with wrong position")
}

// TestVerify_Negative_WrongLeaf flips a bit in the leaf value.
func TestVerify_Negative_WrongLeaf(t *testing.T) {
	rawData := generateRandomData(127)
	p, root := buildProofFromMemtree(t, rawData, 127, 0)

	p.Leaf[0] ^= 0xFF
	require.False(t, Verify(p, root, 0), "Verify should fail with wrong leaf")
}

// TestVerify_Negative_TamperedSibling flips a bit in a proof-path sibling.
func TestVerify_Negative_TamperedSibling(t *testing.T) {
	rawData := generateRandomData(254)
	p, root := buildProofFromMemtree(t, rawData, 254, 0)

	require.True(t, Verify(p, root, 0), "baseline proof should verify")

	p.Proof[0][0] ^= 0xFF
	require.False(t, Verify(p, root, 0), "Verify should fail with tampered sibling")
}

// TestVerify_Negative_TruncatedProof removes the last sibling, producing a
// proof that is one level too short to reach the root.
func TestVerify_Negative_TruncatedProof(t *testing.T) {
	rawData := generateRandomData(254)
	p, root := buildProofFromMemtree(t, rawData, 254, 0)

	p.Proof = p.Proof[:len(p.Proof)-1]
	require.False(t, Verify(p, root, 0), "Verify should fail with truncated proof")
}

// TestVerify_Negative_SwappedSiblings swaps two adjacent proof elements,
// which changes the hashing order at different tree levels.
func TestVerify_Negative_SwappedSiblings(t *testing.T) {
	rawData := generateRandomData(508) // 16 leaves → 4-level proof
	p, root := buildProofFromMemtree(t, rawData, 508, 0)

	require.True(t, Verify(p, root, 0), "baseline proof should verify")
	require.True(t, len(p.Proof) >= 2, "proof should have at least 2 levels")

	p.Proof[0], p.Proof[1] = p.Proof[1], p.Proof[0]
	require.False(t, Verify(p, root, 0), "Verify should fail with swapped siblings")
}

// TestVerify_Negative_OddVsEvenPosition checks that changing the parity of
// the position at each tree level (which flips the left/right concatenation
// order) breaks verification.
//
// Note: Verify() does not bounds-check positions — the PDP contract is
// responsible for that. But any parity change should break the proof.
func TestVerify_Negative_OddVsEvenPosition(t *testing.T) {
	rawData := generateRandomData(254) // 8 leaves
	p, root := buildProofFromMemtree(t, rawData, 254, 0)

	require.True(t, Verify(p, root, 0), "baseline should verify")
	require.False(t, Verify(p, root, 1), "proof for pos 0 should fail at pos 1")
	require.False(t, Verify(p, root, 2), "proof for pos 0 should fail at pos 2")
	require.False(t, Verify(p, root, 3), "proof for pos 0 should fail at pos 3")
}

// TestVerify_Negative_AllZeroProof constructs an all-zero proof and asserts
// it is rejected against a non-zero root.
func TestVerify_Negative_AllZeroProof(t *testing.T) {
	rawData := generateRandomData(127)
	_, root := buildProofFromMemtree(t, rawData, 127, 0)

	p := contract.IPDPTypesProof{
		Leaf:  [32]byte{},
		Proof: [][32]byte{{}, {}},
	}
	require.False(t, Verify(p, root, 0), "Verify should fail with all-zero proof against non-zero root")
}

// TestVerify_CrossPosition proves that a valid proof for one position does NOT
// verify at any other position (for a 16-leaf tree with random data).
func TestVerify_CrossPosition(t *testing.T) {
	const unpaddedSize = abi.UnpaddedPieceSize(508) // 16 leaves
	rawData := generateRandomData(int64(unpaddedSize))

	memtree, root := buildMemtree(t, rawData, unpaddedSize)
	defer pool.Put(memtree)

	nLeaves := int64(unpaddedSize.Padded()) / proof.NODE_SIZE

	for pos := int64(0); pos < nLeaves; pos++ {
		p := extractProof(t, memtree, pos)

		require.True(t, Verify(p, root, uint64(pos)),
			"proof should verify at pos=%d", pos)

		for otherPos := int64(0); otherPos < nLeaves; otherPos++ {
			if otherPos == pos {
				continue
			}
			if Verify(p, root, uint64(otherPos)) {
				t.Errorf("proof for pos=%d unexpectedly verified at pos=%d", pos, otherPos)
			}
		}
	}
}

// =========================================================================
// Section 6: Data-pattern tests
//
// go-fil-commp-hashhash includes test suites for zero-filled and 0xCC-filled
// payloads (testdata/zero.txt, testdata/0xCC.txt). We replicate those data
// patterns here to confirm that our tree builder and Verify() handle
// degenerate inputs correctly.
// =========================================================================

// TestVerify_ZeroData verifies proofs over all-zero payloads.
// Zero payloads exercise the "stacked null padding" path in the tree builder,
// where FR32 padding of zeros produces a specific known leaf pattern.
func TestVerify_ZeroData(t *testing.T) {
	for _, size := range smallUnpaddedSizes[:3] { // 127, 254, 1016
		t.Run(fmt.Sprintf("zeros_%d", size), func(t *testing.T) {
			rawData := make([]byte, size)

			memtree, root := buildMemtree(t, rawData, size)
			defer pool.Put(memtree)

			nLeaves := int64(size.Padded()) / proof.NODE_SIZE
			verifyProofsAtPositions(t, memtree, root, nLeaves,
				fmt.Sprintf("zeros size=%d", size))
		})
	}
}

// TestVerify_0xCCData verifies proofs over constant-0xCC payloads.
// This pattern (10110011 repeated) exercises a different FR32 expansion path
// than zeros or random data, matching go-fil-commp-hashhash's 0xCC.txt suite.
func TestVerify_0xCCData(t *testing.T) {
	for _, size := range smallUnpaddedSizes[:3] { // 127, 254, 1016
		t.Run(fmt.Sprintf("0xCC_%d", size), func(t *testing.T) {
			rawData := bytes.Repeat([]byte{0xCC}, int(size))

			memtree, root := buildMemtree(t, rawData, size)
			defer pool.Put(memtree)

			nLeaves := int64(size.Padded()) / proof.NODE_SIZE
			verifyProofsAtPositions(t, memtree, root, nLeaves,
				fmt.Sprintf("0xCC size=%d", size))
		})
	}
}

// =========================================================================
// Section 7: Structural / helper tests
// =========================================================================

// TestVerify_ProofPathLength asserts that the proof path length (number of
// siblings) equals log2(numLeaves) for each tree size. This is a structural
// invariant of binary Merkle trees.
func TestVerify_ProofPathLength(t *testing.T) {
	tests := []struct {
		unpaddedSize abi.UnpaddedPieceSize
		expectedLen  int // log2(numLeaves)
	}{
		{127, 2},     // 128/32 = 4 leaves → 2-level proof
		{254, 3},     // 256/32 = 8 leaves → 3-level proof
		{508, 4},     // 512/32 = 16 leaves → 4-level proof
		{1016, 5},    // 1024/32 = 32 leaves → 5-level proof
		{2032, 6},    // 2048/32 = 64 leaves → 6-level proof
		{4064, 7},    // 4096/32 = 128 leaves → 7-level proof
		{8128, 8},    // 8192/32 = 256 leaves → 8-level proof
		{16256, 9},   // 16384/32 = 512 leaves → 9-level proof
		{32512, 10},  // 32768/32 = 1024 leaves → 10-level proof
		{65024, 11},  // 65536/32 = 2048 leaves → 11-level proof
		{130048, 12}, // 131072/32 = 4096 leaves → 12-level proof
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("unpadded_%d", tc.unpaddedSize), func(t *testing.T) {
			rawData := generateRandomData(int64(tc.unpaddedSize))
			p, root := buildProofFromMemtree(t, rawData, tc.unpaddedSize, 0)

			require.Len(t, p.Proof, tc.expectedLen,
				"proof path length for size=%d", tc.unpaddedSize)
			require.True(t, Verify(p, root, 0))
		})
	}
}

// TestNextPowerOfTwo validates the nextPowerOfTwo helper used when computing
// padded piece sizes from sub-piece sums.
func TestNextPowerOfTwo(t *testing.T) {
	tests := []struct {
		input    abi.PaddedPieceSize
		expected abi.PaddedPieceSize
	}{
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{128, 128},
		{129, 256},
		{256, 256},
		{1023, 1024},
		{1024, 1024},
		{1025, 2048},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d", tc.input), func(t *testing.T) {
			require.Equal(t, tc.expected, nextPowerOfTwo(tc.input))
		})
	}
}
