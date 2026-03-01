// Tests the Verify() function from task_prove.go and the
// supporting tree-construction machinery it depends on.

package pdpv0

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/savecache"
	"github.com/filecoin-project/curio/pdp/contract"

	"github.com/filecoin-project/lotus/lib/nullreader"
)

// =========================================================================
// Manual tree construction tests
//
// These tests build Merkle trees entirely by hand using sha254() so they
// do not depend on BuildSha254Memtree or MemtreeProof. They serve as an
// independent baseline: if these pass, we know Verify() itself is correct
// for the SHA-254 binary Merkle scheme.
// =========================================================================

// TestVerify_TwoLeafTree builds the simplest possible tree (one hash) and
// verifies both leaf positions.
//
//	   root
//	  /    \
//	L0      L1
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
//	      root
//	    /      \
//	  n01      n23
//	 /   \    /   \
//	L0   L1  L2   L3
func TestVerify_FourLeafTree(t *testing.T) {
	leaves := makeLeaves(4)

	// See diagram above for naming convention: nXY is the parent of leaves X and Y.
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
//	            root
//	        /          \
//	    n0123          n4567
//	    /    \        /    \
//	  n01    n23    n45    n67
//	 / \    / \    / \    / \
//	L0 L1  L2 L3  L4 L5  L6 L7
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
// Memtree roundtrip tests
//
// These tests use BuildSha254Memtree (FR32-padded in-memory tree builder)
// and MemtreeProof (proof extractor) to generate trees from deterministic
// random data (jbenet/go-random, seed 1337), then verify proofs at
// interesting positions via Verify().
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
// commp-hashhash fixture tests
//
// These tests use the known-good commP test vectors published in:
//   https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
//
// The same data is imported here and available at:
// - `smallRandomVectors` and,
// - `largeRandomVectors`
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
			require.Equal(t, vec.commP, root,
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
// savecache cross-validation tests
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
			require.Equal(t, vec.commP, memtreeRoot,
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

// =========================================================================
// Negative tests
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

// HELPERS BELOW THIS LINE -------

// sha254 computes SHA-256 with the two most-significant bits of the last byte
// zeroed, yielding a 254-bit digest. This is the hash function used throughout
// the Filecoin piece-commitment (commP) Merkle tree.
func sha254(data []byte) [32]byte {
	return proof.ComputeBinShaParent(
		[32]byte(data[:32]),
		[32]byte(data[32:64]),
	)
}

// generateRandomData produces deterministic pseudo-random bytes identical to
// the data used by the go-fil-commp-hashhash test suite.
//
// ref: https://github.com/filecoin-project/go-fil-commp-hashhash/blob/c12522866bed8a29785c393fd791c0b9b5605e3e/commp_test.go#L65-L74
func generateRandomData(size int64) []byte {
	const seed = 1337
	rng := rand.New(rand.NewSource(seed))

	buf := make([]byte, size)
	for i := int64(0); i < size; {
		// jbenet/go-random emits one Uint32 and spreads its 4 bytes LSB-first.
		n := rng.Uint32()
		for j := 0; j < 4 && i < size; j++ {
			buf[i] = byte(n & 0xff)
			n >>= 8
			i++
		}
	}
	return buf
}

// buildMemtree builds an in-memory SHA-254 Merkle tree from raw (unpadded)
// data using proof.BuildSha254Memtree and returns both the flat memtree buffer
// and the 32-byte root hash.
//
// BuildSha254Memtree performs FR32 padding internally, so rawData should be
// the unpadded payload.
func buildMemtree(t *testing.T, rawData []byte, unpaddedSize abi.UnpaddedPieceSize) (memtree []byte, root [32]byte) {
	t.Helper()

	mt, err := proof.BuildSha254Memtree(bytes.NewReader(rawData), unpaddedSize)
	require.NoError(t, err)

	r := extractMemtreeRoot(mt)
	return mt, r
}

// buildMemtreeFromPayload builds an in-memory SHA-254 Merkle tree from a
// payload that may be smaller than the padded piece size. Any gap between
// the payload length and the unpadded piece size is zero-filled, exactly as
// Filecoin does when a piece is smaller than its declared size.
func buildMemtreeFromPayload(t *testing.T, payload []byte, paddedSize uint64) (memtree []byte, root [32]byte) {
	t.Helper()

	unpaddedSize := abi.PaddedPieceSize(paddedSize).Unpadded()
	reader := io.LimitReader(
		io.MultiReader(
			bytes.NewReader(payload),
			&nullreader.Reader{},
		),
		int64(unpaddedSize),
	)

	mt, err := proof.BuildSha254Memtree(reader, unpaddedSize)
	require.NoError(t, err)

	r := extractMemtreeRoot(mt)
	return mt, r
}

// extractMemtreeRoot returns the root hash from a flat memtree buffer.
func extractMemtreeRoot(memtree []byte) [32]byte {
	totalNodes := int64(len(memtree)) / proof.NODE_SIZE
	rootOffset := (totalNodes - 1) * proof.NODE_SIZE
	var root [32]byte
	copy(root[:], memtree[rootOffset:rootOffset+proof.NODE_SIZE])
	return root
}

// buildProofFromMemtree builds an in-memory tree, extracts a Merkle proof for
// the given leaf index, and returns both the proof (in contract-compatible
// form) and the tree root.
func buildProofFromMemtree(t *testing.T, rawData []byte, unpaddedSize abi.UnpaddedPieceSize, leafIndex int64) (contract.IPDPTypesProof, [32]byte) {
	t.Helper()

	memtree, root := buildMemtree(t, rawData, unpaddedSize)
	defer pool.Put(memtree)

	return extractProof(t, memtree, leafIndex), root
}

// extractProof extracts a Merkle proof for a single leaf from a flat memtree.
func extractProof(t *testing.T, memtree []byte, leafIndex int64) contract.IPDPTypesProof {
	t.Helper()

	rawProof, err := proof.MemtreeProof(memtree, leafIndex)
	require.NoError(t, err)

	return contract.IPDPTypesProof{
		Leaf:  rawProof.Leaf,
		Proof: rawProof.Proof,
	}
}

// verifyProofsAtPositions generates and verifies Merkle proofs at a set of
// "interesting" leaf positions within the given memtree. This is the workhorse
// used by most roundtrip tests. It asserts that every proof passes Verify().
func verifyProofsAtPositions(t *testing.T, memtree []byte, root [32]byte, nLeaves int64, label string) {
	t.Helper()

	for _, pos := range interestingPositions(nLeaves) {
		p := extractProof(t, memtree, pos)
		require.True(t, Verify(p, root, uint64(pos)),
			"Verify failed %s pos=%d", label, pos)
	}
}

// interestingPositions returns a deduplicated set of leaf indices that exercise
// various parts of a binary Merkle tree:
//
//   - Boundary leaves (first, last, second-to-last).
//   - Power-of-two boundaries (quarter, half, three-quarter) which sit at
//     subtree roots and stress left-vs-right sibling selection.
//   - Off-by-one positions around those boundaries to catch fence-post errors.
//   - A few random positions to increase coverage, seeded by nLeaves for variety
//
// These positions model the kinds of challenges the PDP verifier contract can
// generate.
func interestingPositions(nLeaves int64) []int64 {
	const (
		randomPositionsMax = 5
		randomizerSeed     = 9009
	)
	candidates := []int64{
		0,               // first leaf
		1,               // second leaf
		nLeaves/4 - 1,   // just before quarter boundary
		nLeaves / 4,     // quarter boundary
		nLeaves/2 - 1,   // just before midpoint
		nLeaves / 2,     // midpoint
		nLeaves/2 + 1,   // just after midpoint
		nLeaves*3/4 - 1, // just before three-quarter
		nLeaves * 3 / 4, // three-quarter boundary
		nLeaves - 2,     // second-to-last leaf
		nLeaves - 1,     // last leaf
	}

	// Add a few random positions to increase coverage, ensuring we don't exceed nLeaves.
	rng := rand.New(rand.NewSource(randomizerSeed + nLeaves)) // seed depends on nLeaves for variety across tree sizes
	for range randomPositionsMax {
		randomPosWithinRange := rng.Int63n(nLeaves)
		candidates = append(candidates, randomPosWithinRange)
	}

	seen := make(map[int64]bool, len(candidates))
	result := make([]int64, 0, len(candidates))
	for _, p := range candidates {
		if p >= 0 && p < nLeaves && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

// makeLeaf creates a deterministic 32-byte leaf value, with deterministic content.
func makeLeaf(leafIndex int) [32]byte {
	var leaf [32]byte
	for j := range leaf {
		leaf[j] = byte(leafIndex*32 + j)
	}
	return leaf
}

// makeLeaves creates n deterministic leaves using makeLeaf.
func makeLeaves(n int) [][32]byte {
	leaves := make([][32]byte, n)
	for i := 0; i < n; i++ {
		leaves[i] = makeLeaf(i)
	}
	return leaves
}

// commPTestVector holds one row from the go-fil-commp-hashhash test fixture
// files. Each row is: unpadded_payload_size, padded_piece_size, commP_CID.
//
// Source file:
//
//	https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
type commPTestVector struct {
	payloadSize int64
	paddedSize  uint64
	commP       [32]byte
}

// parseTestVectors parses newline-separated test vectors in the format:
//
//	payload_size,padded_size,commP_CID
//
// This matches the format of go-fil-commp-hashhash/testdata/random.txt.
func parseTestVectors(data string) ([]commPTestVector, error) {
	var vectors []commPTestVector
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			continue
		}
		var v commPTestVector
		if _, err := fmt.Sscanf(parts[0], "%d", &v.payloadSize); err != nil {
			return nil, fmt.Errorf("parsing payload size %q: %w", parts[0], err)
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &v.paddedSize); err != nil {
			return nil, fmt.Errorf("parsing padded size %q: %w", parts[1], err)
		}

		// CID format: 'b' multibase prefix + base32-lower-encoded CIDv1.
		// cid.Decode handles the multibase prefix automatically.
		c, err := cid.Decode(parts[2])
		if err != nil {
			return nil, fmt.Errorf("decoding CID %q: %w", parts[2], err)
		}
		decoded, err := mh.Decode(c.Hash())
		if err != nil {
			return nil, fmt.Errorf("decoding multihash for CID %q: %w", parts[2], err)
		}
		copy(v.commP[:], decoded.Digest)
		vectors = append(vectors, v)
	}
	return vectors, nil
}

// ---------------------------------------------------------------------------
// Test vector data
//
// These are subsets of go-fil-commp-hashhash/testdata/random.txt, the
// canonical Filecoin commP test fixture. The data corresponding to each
// vector is produced by generateRandomData (seed=1337, jbenet/go-random
// algorithm).
//
// We select vectors whose padded sizes fit within proof.MaxMemtreeSize so we
// can build the full binary tree in memory and generate + verify Merkle proofs
// end-to-end.
//
// Source:
//   https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
// ---------------------------------------------------------------------------

// smallRandomVectors: payload sizes <= 2032 bytes (padded sizes <= 2048).
// These are fast enough to run on every `go test` invocation.
const smallRandomVectors = `96,128,baga6ea4seaqo4jg2quvjpclvaoicqywb26mnjlme54tkeb3e3ceeahbyg7tviai
126,128,baga6ea4seaqein3inkr73gcpqxdhjy56wfkb52pdw23hpuwr35csiiso2yuvyoq
127,128,baga6ea4seaqmqqwwyg6ql6scq5vvunlbvddhjxwzqsxcdnig4vqbuyh6bbzxodi
192,256,baga6ea4seaqotbhavqkao7eoxjtixti2glidgwvxp3chvidrlf52l7vaknwweky
253,256,baga6ea4seaqebawcodewpyhscsdthmmtvgqkzogca45eg6v7lafkt4tv434l6iq
254,256,baga6ea4seaqpt3iccipvltny4uvpd37qg6w5u7wbng7lqbqplbp7t4amwgvmefi
255,512,baga6ea4seaqlm52frc6d6xnpfquc75qxq4r5pinbahr2klui4fqs5wxy3doyaaq
256,512,baga6ea4seaqffn52ixzqoaahrdl7pahrljpvy45xaosmt35ty53gyvai6t456ni
384,512,baga6ea4seaqnory3ske3s5l25ykklefj5ph4ymer6bt42hmw55rme37jkkys6li
507,512,baga6ea4seaqjplkgedsxnqoygmbx2iqvqog4w2scn2dzpxw26iggia357jveuka
508,512,baga6ea4seaqidun3iro3tn65tqn3zjfwa2j2bltromfptcdqa5stlkjh5q4qeaa
509,1024,baga6ea4seaqky7sahpzp37csmkswze43zbpyzrpdli5mxzy7k5pflwridmnkgmi
512,1024,baga6ea4seaqiqj7rbfe4pz6b7rcbf3dv6xfnnr2dwbwotr6xitwec5mrgslgcea
768,1024,baga6ea4seaqp47zaubuxxi6sdjd7qdr5wzcutem2stj5pec3g554ync227336cy
1015,1024,baga6ea4seaqedz5ubjpdwgfm72tn6ejrnejs2wcvhycf3enyy6ygzwu4pulwijy
1016,1024,baga6ea4seaqnvlx4oaqkcjrog6v27jpouto6rbsymqvljftyxitm7jfalzpmwcq
1017,2048,baga6ea4seaqhx2zkltpeqwlnkvxfu2auzwv6xeqk35gd2keih65iocpjnrsqeoi
1024,2048,baga6ea4seaqjvo24o24lunggsgxp2ykw2nl6ub5oshkgyccaiscbiu7dixiwiga
1536,2048,baga6ea4seaqakth6qnuusqztecom6tlwcbp2m5a3gecgomgnc2z6qxq3fe4jggy
2031,2048,baga6ea4seaqnk4aqvhnzrlkxas5buothhuplfecjo2lvvj3bwn6j27mdj2hw6lq
2032,2048,baga6ea4seaqiqu4swshjvltped5eygatfnwt4rjnxfizqlsnlt2x4kxqpflmsay`

// largerRandomVectors: payload sizes > 2032 up to 131072 bytes (padded up to
// 131072). These are still within proof.MaxMemtreeSize but take longer to
// process, so they are skipped by `go test -short`.
const largerRandomVectors = `2033,4096,baga6ea4seaqfvubqe7mwtotokoo3tkephbpi3xoppi7425icaqvxybj27wm2oka
2048,4096,baga6ea4seaqj5uibbzvsimuiiy6jl2hfhhyl4jzr6ihsiwhqjpfemj2qo7q42bq
3072,4096,baga6ea4seaqkpxrivvwqvuyi4ofwcgtq3hzkyfmjjlb56p77mb2tbn6pmumlany
4063,4096,baga6ea4seaqbderkod5u2oipurx4pnnzwpmbzgnsju3jgczuqivyujfgtixj6iy
4064,4096,baga6ea4seaqn6j5vf3udiid4rbb6rv2ek2s7o3dgnlfvamb3dt5qe6o6x2pnoka
4065,8192,baga6ea4seaqaej5ao5dat4biovfbx2h2exoivsoe4bwbhhajnop4tauoue7tyoa
4096,8192,baga6ea4seaqfc5qcik7yruh4gzadmgrdxtyjoqyxabv5545okfshdrkh4ikr2ba
6144,8192,baga6ea4seaqlf6nkbnjvxinjggutsjkow7dbxussjj3puax2rhzasdhxv4dn6mq
8127,8192,baga6ea4seaqpaqnhn4rce6gsdimm5vl5v6izkh5f6lagbeheoiudqodky45oiaa
8128,8192,baga6ea4seaqotwrimtqoatryhi3zmcjvykr3uyybsx6n5fmdq2wgo6zw7nz3kiq
8129,16384,baga6ea4seaqjkxyydkhqeqzgtf3vsmwnsryyqnchnsytfnanfnnqevirf4cpopy
8192,16384,baga6ea4seaqbmiwvd7oeq5nse5xqy65woqosxoceskcxwsbbyn3pnxydmqztmpi
12288,16384,baga6ea4seaqmtdtep5xkmkavglidtslgcsjy32ej54ib2riarjy62x52kze2ihy
16255,16384,baga6ea4seaqmguzq2wp33mxdia3wq3vaahijqcw4svwbga2abgeccofvf6u5cmy
16256,16384,baga6ea4seaqdborfbr4mlgghf4ud6vo7wjkgmvc3elanqh4miez4wd7xfprl6cy
16257,32768,baga6ea4seaqhtxok7ntvuzk42rh6lmnvwioqvtenrp7tymumicoaelabizfnmha
16384,32768,baga6ea4seaqbq56qsxemtyzd3zcsjoetht7swqir7rw4fpmn6g6cfngz2djxoji
32512,32768,baga6ea4seaqhfhque5zrqd3tg62uo2ruka2lzekpn46ahlsgl3nat4uoojuyemq
32513,65536,baga6ea4seaqizdw52alqml4tyt37bqtuqisewhpz64fvlmryg7ehvdibkptqmga
32768,65536,baga6ea4seaqaxucpakjt27aafpdbrecklvae7kxj3ha6dpdo45rpm7wjzls7sdy
65024,65536,baga6ea4seaqgjaavyognzadlkkcwaovy4ij7grb3eso65dqvwkiqfg5g3cb3qbi
65025,131072,baga6ea4seaqapw7q3joywtwplratzjnyzxfsi5nuemcgtyqapmd35sdyjtnfkpa
65536,131072,baga6ea4seaqarj3zpcpxjs256fzt2zmral6cy2g6wqwtvcawfayhql7twnrgioa`

// smallUnpaddedSizes are unpadded piece sizes whose padded forms are small
// enough to build full memtrees and verify every leaf position exhaustively.
// They also happen to be the exact sizes for which FR32 padding produces the
// most compact power-of-two padded pieces (unpadded = padded * 127/128).
var smallUnpaddedSizes = []abi.UnpaddedPieceSize{127, 254, 508, 1016}

// wideUnpaddedSizes span the range from the smallest to the largest piece that
// fits in proof.MaxMemtreeSize. Used for roundtrip proof/verify tests where
// exhaustive leaf enumeration would be too slow.
var wideUnpaddedSizes = []abi.UnpaddedPieceSize{
	127,      // 4 leaves
	254,      // 8 leaves
	508,      // 16 leaves
	1016,     // 32 leaves
	2032,     // 64 leaves
	4064,     // 128 leaves
	8128,     // 256 leaves
	16256,    // 512 leaves
	32512,    // 1024 leaves
	65024,    // 2048 leaves
	130048,   // 4096 leaves
	1040384,  // 32768 leaves
	2080768,  // 65536 leaves
	4161536,  // 131072 leaves
	8323072,  // 262144 leaves
	16646144, // 524288 leaves
}
