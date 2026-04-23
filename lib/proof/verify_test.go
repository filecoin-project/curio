package proof_test

import (
	"bytes"
	"fmt"
	"testing"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/savecache"
)

// TestManualEightLeafTree builds an 8-leaf tree by hand using ComputeBinShaParent
// and verifies all 8 leaf positions. Independent of BuildSha254Memtree/MemtreeProof.
//
//	            root
//	        /          \
//	    n0123          n4567
//	    /    \        /    \
//	  n01    n23    n45    n67
//	 / \    / \    / \    / \
//	L0 L1  L2 L3  L4 L5  L6 L7
func TestManualEightLeafTree(t *testing.T) {
	leaves := makeLeaves(8)

	// Level 1: pair siblings
	n01 := proof.ComputeBinShaParent(leaves[0], leaves[1])
	n23 := proof.ComputeBinShaParent(leaves[2], leaves[3])
	n45 := proof.ComputeBinShaParent(leaves[4], leaves[5])
	n67 := proof.ComputeBinShaParent(leaves[6], leaves[7])

	// Level 2
	n0123 := proof.ComputeBinShaParent(n01, n23)
	n4567 := proof.ComputeBinShaParent(n45, n67)

	// Root
	root := proof.ComputeBinShaParent(n0123, n4567)

	tests := []struct {
		pos      uint64
		leaf     [32]byte
		siblings [][32]byte
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
		ok := proof.VerifyProof(tc.leaf, tc.siblings, root, tc.pos)
		require.True(t, ok, "verify failed at position %d", tc.pos)
	}
}

// TestSavecacheRootsMatchFixtures validates that savecache.Calc produces roots
// matching the externally-anchored CommP fixtures from go-fil-commp-hashhash.
func TestSavecacheRootsMatchFixtures(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	for _, fx := range fixtures {
		t.Run(fmt.Sprintf("payload_%d", fx.PayloadSize), func(t *testing.T) {
			t.Parallel()
			data := generateDeterministicData(fx.PayloadSize)

			calc := &savecache.Calc{}
			_, err := calc.Write(data)
			require.NoError(t, err)

			var root [32]byte
			copy(root[:], calc.Sum(nil))
			require.Equal(t, fx.RawCommP, root,
				"savecache root should match fixture CommP")
		})
	}
}

// TestNegative_WrongLeaf flips a bit in the leaf value.
func TestNegative_WrongLeaf(t *testing.T) {
	memtree, root, _ := buildSmallMemtree(t)
	defer pool.Put(memtree)

	prf, err := proof.MemtreeProof(memtree, 0)
	require.NoError(t, err)

	prf.Leaf[0] ^= 0xFF
	require.False(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0),
		"should fail with wrong leaf")
}

// TestNegative_TamperedSibling flips a bit in a proof-path sibling.
func TestNegative_TamperedSibling(t *testing.T) {
	memtree, root, _ := buildSmallMemtree(t)
	defer pool.Put(memtree)

	prf, err := proof.MemtreeProof(memtree, 0)
	require.NoError(t, err)
	require.True(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0))

	prf.Proof[0][0] ^= 0xFF
	require.False(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0),
		"should fail with tampered sibling")
}

// TestNegative_TruncatedProof removes the last sibling, producing a proof
// that is one level too short to reach the root.
func TestNegative_TruncatedProof(t *testing.T) {
	memtree, root, _ := buildSmallMemtree(t)
	defer pool.Put(memtree)

	prf, err := proof.MemtreeProof(memtree, 0)
	require.NoError(t, err)

	prf.Proof = prf.Proof[:len(prf.Proof)-1]
	require.False(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0),
		"should fail with truncated proof")
}

// TestNegative_SwappedSiblings swaps two adjacent proof elements, changing
// the hashing order at different tree levels.
func TestNegative_SwappedSiblings(t *testing.T) {
	memtree, root, _ := buildSmallMemtree(t)
	defer pool.Put(memtree)

	prf, err := proof.MemtreeProof(memtree, 0)
	require.NoError(t, err)
	require.True(t, len(prf.Proof) >= 2, "need at least 2 levels")
	require.True(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0))

	prf.Proof[0], prf.Proof[1] = prf.Proof[1], prf.Proof[0]
	require.False(t, proof.VerifyProof(prf.Leaf, prf.Proof, root, 0),
		"should fail with swapped siblings")
}

// --- helpers ---

// makeLeaves creates n deterministic 32-byte leaf values.
func makeLeaves(n int) [][32]byte {
	leaves := make([][32]byte, n)
	for i := range leaves {
		for j := range leaves[i] {
			leaves[i][j] = byte(i*32 + j)
		}
	}
	return leaves
}

// buildSmallMemtree builds a memtree from 508 bytes of deterministic data
// (16 leaves, 4-level proof), returning the memtree buffer, root, and leaf count.
func buildSmallMemtree(t *testing.T) ([]byte, [32]byte, int64) {
	t.Helper()
	data := generateDeterministicData(508)
	memtree, err := proof.BuildSha254Memtree(
		bytes.NewReader(data), 508)
	require.NoError(t, err)
	root := extractRoot(memtree)
	nLeaves := int64(len(memtree)/proof.NODE_SIZE+1) / 2
	return memtree, root, nLeaves
}
