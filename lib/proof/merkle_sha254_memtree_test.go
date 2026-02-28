package proof_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	randmath "math/rand"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/savecache"
)

// Selected fixtures from go-fil-commp-hashhash testdata/random.txt.
// Seed-1337 deterministic random data (identical to jbenet/go-random).
// CommP roots validated against filecoin-ffi (Rust reference implementation).
var commPFixtures = []struct {
	PayloadSize int64
	PieceSize   uint64
	CID         string
}{
	// Small: minimal valid sizes and power-of-2 boundaries
	{127, 128, "baga6ea4seaqmqqwwyg6ql6scq5vvunlbvddhjxwzqsxcdnig4vqbuyh6bbzxodi"},
	{1016, 1024, "baga6ea4seaqnvlx4oaqkcjrog6v27jpouto6rbsymqvljftyxitm7jfalzpmwcq"},
	{1017, 2048, "baga6ea4seaqhx2zkltpeqwlnkvxfu2auzwv6xeqk35gd2keih65iocpjnrsqeoi"},
	// Medium: representative tree sizes
	{8128, 8192, "baga6ea4seaqotwrimtqoatryhi3zmcjvykr3uyybsx6n5fmdq2wgo6zw7nz3kiq"},
	{65024, 65536, "baga6ea4seaqgjaavyognzadlkkcwaovy4ij7grb3eso65dqvwkiqfg5g3cb3qbi"},
	// Large: production-relevant sizes with zero-padding and full-fill
	{786432, 1048576, "baga6ea4seaqdqltvzfcyx5nu3o6yazc6ftuxt6xkhwjywatjmjrbj6shwsqlcaq"},
	{1040384, 1048576, "baga6ea4seaqio7pi5twddpb4qevcestrwtc77nuou7o2xilhkhkj46irbsolaoq"},
	{4161536, 4194304, "baga6ea4seaqoarx7s4i2azmftkspmosawixgqg3frdo3iugalasu5burrgu3qbq"},
	// 32 MiB padded: the production cache threshold (MinSizeForCache)
	{33292288, 33554432, "baga6ea4seaqgseawqou6toeg5spmgnjsamjiqq2bhbamfjtj5e67ytnncwuasiq"},
	// 128 MiB padded
	{133169152, 134217728, "baga6ea4seaqk3bkri6x5kqcxezyq63v7ct37rhlsb7pgynmtifmwqivbhvq4why"},
	// 512 MiB padded: exercises large tree builds (~1 GiB memtree)
	{532676608, 536870912, "baga6ea4seaqexfdfvlgmue3jn6wmpyfgqq3itkdyae2vz3bw6tzcsrddlx7x6my"},
}

type commPFixture struct {
	PayloadSize int64
	PieceSize   uint64
	RawCommP    [32]byte
}

func loadCommPFixtures(t *testing.T) []commPFixture {
	t.Helper()
	fixtures := make([]commPFixture, len(commPFixtures))
	for i, sf := range commPFixtures {
		c, err := cid.Decode(sf.CID)
		require.NoError(t, err, "decoding CID for fixture %d", i)
		commP, err := commcid.CIDToPieceCommitmentV1(c)
		require.NoError(t, err, "extracting CommP for fixture %d", i)
		fixtures[i] = commPFixture{
			PayloadSize: sf.PayloadSize,
			PieceSize:   sf.PieceSize,
		}
		copy(fixtures[i].RawCommP[:], commP)
	}
	return fixtures
}

// generateDeterministicData produces bytes identical to jbenet/go-random with seed 1337.
// This matches the data generation used by go-fil-commp-hashhash test fixtures.
func generateDeterministicData(size int64) []byte {
	rng := randmath.New(randmath.NewSource(1337))
	buf := make([]byte, size)
	for i := int64(0); i < size; {
		n := rng.Uint32()
		for j := 0; j < 4 && i < size; j++ {
			buf[i] = byte(n & 0xff)
			n >>= 8
			i++
		}
	}
	return buf
}

func extractRoot(memtree []byte) [32]byte {
	var root [32]byte
	copy(root[:], memtree[len(memtree)-proof.NODE_SIZE:])
	return root
}

// Note on proof.VerifyProof circularity:
//
// Using VerifyProof to validate MemtreeProof output is circular in isolation:
// both implement the same algorithm, so a shared bug could pass undetected.
// This is acceptable here because:
//  1. The root being verified against is externally anchored -- commPFixtures
//     are validated against filecoin-ffi (Rust reference), not our Go code.
//  2. VerifyProof is 15 lines of trivially auditable hash-and-combine logic,
//     a Go translation of the on-chain MerkleVerify.processInclusionProofMemory.
//  3. For extracted proofs to accidentally verify against the correct externally-
//     anchored root across multiple leaf positions and piece sizes is
//     computationally infeasible.
//  4. Negative tests (wrong root, wrong position) confirm the verifier actually
//     rejects bad inputs.
//  5. The on-chain PDPVerifier (Solidity) has its own independent Verify
//     implementation that provides a third cross-check in integration.

// buildMemtree generates deterministic data and builds a SHA254 memtree for the
// given fixture. The data is zero-padded to the unpadded piece size before FR32 expansion.
func buildMemtree(t *testing.T, fx commPFixture) []byte {
	t.Helper()
	data := generateDeterministicData(fx.PayloadSize)
	unpaddedSize := abi.PaddedPieceSize(fx.PieceSize).Unpadded()
	var reader io.Reader
	if int64(unpaddedSize) > fx.PayloadSize {
		reader = io.MultiReader(
			bytes.NewReader(data),
			bytes.NewReader(make([]byte, int64(unpaddedSize)-fx.PayloadSize)),
		)
	} else {
		reader = bytes.NewReader(data)
	}
	memtree, err := proof.BuildSha254Memtree(reader, unpaddedSize)
	require.NoError(t, err)
	return memtree
}

// TestMemtreeRootsMatchCommPFixtures validates that BuildSha254Memtree produces
// trees whose roots match externally validated CommP values from go-fil-commp-hashhash.
// This establishes an external trust anchor: the fixtures are validated against
// filecoin-ffi (the Rust reference). If this test passes, the FR32 padding, SHA254
// hashing, and tree construction are all correct.
func TestMemtreeRootsMatchCommPFixtures(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	for _, fx := range fixtures {
		fx := fx
		t.Run(fmt.Sprintf("payload_%d", fx.PayloadSize), func(t *testing.T) {
			t.Parallel()
			memtree := buildMemtree(t, fx)
			root := extractRoot(memtree)
			require.Equal(t, fx.RawCommP, root,
				"memtree root should match commp-hashhash fixture CommP")
		})
	}
}

// TestProofRoundTrip validates that MemtreeProof extracts correct proofs that
// VerifyProof confirms, across multiple fixture sizes and strategic leaf positions.
// Tests both positive (correct root/position) and negative (wrong root, wrong position).
func TestProofRoundTrip(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	for _, fx := range fixtures {
		fx := fx
		t.Run(fmt.Sprintf("payload_%d", fx.PayloadSize), func(t *testing.T) {
			t.Parallel()
			memtree := buildMemtree(t, fx)
			root := extractRoot(memtree)
			require.Equal(t, fx.RawCommP, root)

			nLeaves := int64(fx.PieceSize) / proof.NODE_SIZE
			challenges := []int64{0}
			if nLeaves > 1 {
				challenges = append(challenges,
					1,           // second leaf (right-child at level 0)
					nLeaves-1,   // last leaf
					nLeaves/2,   // middle
					nLeaves/2-1, // just before middle (power-of-2 boundary)
				)
			}

			for _, pos := range challenges {
				prf, err := proof.MemtreeProof(memtree, pos)
				require.NoError(t, err, "proof extraction at leaf %d", pos)
				require.Equal(t, root, prf.Root, "proof root at leaf %d", pos)

				ok := proof.VerifyProof(prf.Leaf, prf.Proof, root, uint64(pos))
				require.True(t, ok, "verify should pass at leaf %d", pos)

				// Wrong root should fail
				badRoot := root
				badRoot[0] ^= 0xFF
				ok = proof.VerifyProof(prf.Leaf, prf.Proof, badRoot, uint64(pos))
				require.False(t, ok, "verify should fail with wrong root at leaf %d", pos)

				// Wrong position should fail (skip if tree too small for meaningful test)
				if nLeaves > 2 {
					wrongPos := (pos + 1) % nLeaves
					ok = proof.VerifyProof(prf.Leaf, prf.Proof, root, uint64(wrongPos))
					require.False(t, ok, "verify should fail with wrong position at leaf %d", pos)
				}
			}
		})
	}
}

// TestCacheSplitProof validates the cached split proof pipeline step-by-step
// using ComputeCacheProofParams, CombineSplitProofs, and savecache snapshot layers.
// Intermediate assertions at each stage pinpoint which step breaks on failure.
// Complements TestGenerateCachedProof which tests the same pipeline as a black box.
func TestCacheSplitProof(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	for _, fx := range fixtures {
		// Minimum 256 leaves (8 KiB padded) for a meaningful snapshot layer.
		// With test-mode layer index 6, this gives 4+ snapshot nodes.
		if fx.PieceSize < 8192 {
			continue
		}
		fx := fx
		t.Run(fmt.Sprintf("payload_%d", fx.PayloadSize), func(t *testing.T) {
			t.Parallel()

			// Build full memtree and validate root
			fullMemtree := buildMemtree(t, fx)
			root := extractRoot(fullMemtree)
			require.Equal(t, fx.RawCommP, root)

			// Compute snapshot via savecache (test mode: ~2 KiB per snapshot node)
			cp := savecache.NewCommPWithSizeForTest(uint64(fx.PayloadSize))
			_, err := io.Copy(cp, bytes.NewReader(generateDeterministicData(fx.PayloadSize)))
			require.NoError(t, err)

			commP, _, layerIdx, expectedNodeCount, snapshotNodes, err := cp.DigestWithSnapShot()
			require.NoError(t, err)

			var snapCommP [32]byte
			copy(snapCommP[:], commP)
			require.Equal(t, fx.RawCommP, snapCommP, "savecache CommP should match fixture")
			require.Equal(t, expectedNodeCount, len(snapshotNodes))

			// Build upper memtree from snapshot layer
			snapshotData := make([]byte, len(snapshotNodes)*proof.NODE_SIZE)
			for i, node := range snapshotNodes {
				copy(snapshotData[i*proof.NODE_SIZE:], node.Hash[:])
			}
			upperMemtree, err := proof.BuildSha254MemtreeFromSnapshot(snapshotData)
			require.NoError(t, err)
			upperRoot := extractRoot(upperMemtree)
			require.Equal(t, root, upperRoot, "upper memtree root should match full tree root")

			// Prepare full unpadded data for section extraction
			unpaddedSize := abi.PaddedPieceSize(fx.PieceSize).Unpadded()
			fullData := generateDeterministicData(fx.PayloadSize)
			if int64(unpaddedSize) > fx.PayloadSize {
				fullData = append(fullData, make([]byte, int64(unpaddedSize)-fx.PayloadSize)...)
			}

			nLeaves := int64(fx.PieceSize) / proof.NODE_SIZE
			sampleParams := proof.ComputeCacheProofParams(layerIdx, 0)
			nSections := int64(len(snapshotNodes))

			challenges := []int64{
				0,                              // first leaf of first section
				sampleParams.LeavesPerNode - 1, // last leaf of first section
				nLeaves / 2,                    // middle of tree
				nLeaves - 1,                    // last leaf of last section
			}

			for _, challenge := range challenges {
				params := proof.ComputeCacheProofParams(layerIdx, challenge)

				require.Less(t, params.SnapshotNodeIndex, nSections,
					"challenge %d maps to section %d but only %d sections exist",
					challenge, params.SnapshotNodeIndex, nSections)

				// Build sub-memtree for this section using production-computed offsets
				sectionBuf := make([]byte, params.SectionLength)
				copy(sectionBuf, fullData[params.SectionOffset:params.SectionOffset+params.SectionLength])

				subMemtree, err := proof.BuildSha254Memtree(
					bytes.NewReader(sectionBuf), params.SubrootSize.Unpadded())
				require.NoError(t, err)

				// Sub-memtree root must match the corresponding snapshot node
				subRoot := extractRoot(subMemtree)
				require.Equal(t, snapshotNodes[params.SnapshotNodeIndex].Hash, subRoot,
					"sub-memtree root should match snapshot node at section %d (challenge %d)",
					params.SnapshotNodeIndex, challenge)

				// Extract sub-proof (leaf -> snapshot node)
				subProof, err := proof.MemtreeProof(subMemtree, params.SubTreeChallenge)
				require.NoError(t, err)

				// Extract upper proof (snapshot node -> root)
				upperProof, err := proof.MemtreeProof(upperMemtree, params.SnapshotNodeIndex)
				require.NoError(t, err)

				// Combine sub-proof and upper proof into a full proof from leaf to root
				combinedProof := proof.CombineSplitProofs(subProof, upperProof)

				// Verify the combined split proof
				ok := proof.VerifyProof(combinedProof.Leaf, combinedProof.Proof, root, uint64(challenge))
				require.True(t, ok,
					"split proof should verify at challenge %d (section %d, local %d)",
					challenge, params.SnapshotNodeIndex, params.SubTreeChallenge)

				// Cross-check: leaf hash should match the full memtree's leaf
				directProof, err := proof.MemtreeProof(fullMemtree, challenge)
				require.NoError(t, err)
				require.Equal(t, directProof.Leaf, subProof.Leaf,
					"leaf mismatch between split and direct proof at challenge %d", challenge)

				// Cross-check: direct (non-split) proof should also verify
				ok = proof.VerifyProof(directProof.Leaf, directProof.Proof, root, uint64(challenge))
				require.True(t, ok, "direct proof should verify at challenge %d", challenge)
			}
		})
	}
}

// --- Mock implementations for GenerateCachedProof testing ---

// memPieceReader implements proof.PieceReader backed by in-memory data.
type memPieceReader struct {
	data map[string][]byte // v1 CID string -> unpadded data
}

type bytesReadCloser struct {
	*bytes.Reader
}

func (b *bytesReadCloser) Close() error { return nil }

func (r *memPieceReader) GetPieceReader(_ context.Context, pieceCid cid.Cid) (proof.SectionReadCloser, abi.UnpaddedPieceSize, error) {
	data, ok := r.data[pieceCid.String()]
	if !ok {
		return nil, 0, fmt.Errorf("piece not found: %s", pieceCid)
	}
	return &bytesReadCloser{bytes.NewReader(data)}, abi.UnpaddedPieceSize(len(data)), nil
}

// memProofCache implements proof.ProofCache backed by in-memory snapshot data.
type memProofCache struct {
	layerIdx int
	data     []byte // concatenated 32-byte node hashes
}

func newMemProofCache(layerIdx int, nodes [][32]byte) *memProofCache {
	data := make([]byte, len(nodes)*proof.NODE_SIZE)
	for i, n := range nodes {
		copy(data[i*proof.NODE_SIZE:], n[:])
	}
	return &memProofCache{layerIdx: layerIdx, data: data}
}

func (c *memProofCache) GetLayerIndex(_ context.Context, _ cid.Cid) (bool, int, error) {
	if len(c.data) == 0 {
		return false, 0, nil
	}
	return true, c.layerIdx, nil
}

func (c *memProofCache) GetNode(_ context.Context, _ cid.Cid, _ int, index int64) (bool, [32]byte, error) {
	nNodes := int64(len(c.data) / proof.NODE_SIZE)
	if index < 0 || index >= nNodes {
		return false, [32]byte{}, nil
	}
	var hash [32]byte
	copy(hash[:], c.data[index*proof.NODE_SIZE:(index+1)*proof.NODE_SIZE])
	return true, hash, nil
}

func (c *memProofCache) GetLayer(_ context.Context, _ cid.Cid, _ int) ([]byte, error) {
	return c.data, nil
}

// TestGenerateCachedProof exercises GenerateCachedProof as a black box with mock
// PieceReader and ProofCache implementations, verifying end-to-end correctness.
// Complements TestCacheSplitProof which tests the same pipeline with intermediate assertions.
func TestGenerateCachedProof(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	for _, fx := range fixtures {
		// Same threshold as TestCacheSplitProof: need enough leaves for snapshot layer
		if fx.PieceSize < 8192 {
			continue
		}
		fx := fx
		t.Run(fmt.Sprintf("payload_%d", fx.PayloadSize), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			// Build full memtree for cross-checking
			fullMemtree := buildMemtree(t, fx)
			root := extractRoot(fullMemtree)
			require.Equal(t, fx.RawCommP, root)

			// Compute snapshot via savecache (test mode)
			cp := savecache.NewCommPWithSizeForTest(uint64(fx.PayloadSize))
			_, err := io.Copy(cp, bytes.NewReader(generateDeterministicData(fx.PayloadSize)))
			require.NoError(t, err)

			commP, _, layerIdx, _, snapshotNodes, err := cp.DigestWithSnapShot()
			require.NoError(t, err)

			var snapCommP [32]byte
			copy(snapCommP[:], commP)
			require.Equal(t, fx.RawCommP, snapCommP)

			// Construct v2 CID from fixture data
			pieceCidV2, err := commcid.DataCommitmentToPieceCidv2(fx.RawCommP[:], uint64(fx.PayloadSize))
			require.NoError(t, err)

			// Derive v1 CID for piece reader keying
			pieceCidV1, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
			require.NoError(t, err)

			// Prepare full unpadded data
			unpaddedSize := abi.PaddedPieceSize(fx.PieceSize).Unpadded()
			fullData := generateDeterministicData(fx.PayloadSize)
			if int64(unpaddedSize) > fx.PayloadSize {
				fullData = append(fullData, make([]byte, int64(unpaddedSize)-fx.PayloadSize)...)
			}

			// Build mock PieceReader
			reader := &memPieceReader{
				data: map[string][]byte{
					pieceCidV1.String(): fullData,
				},
			}

			// Build mock ProofCache from savecache output
			nodeHashes := make([][32]byte, len(snapshotNodes))
			for i, n := range snapshotNodes {
				nodeHashes[i] = n.Hash
			}
			cache := newMemProofCache(layerIdx, nodeHashes)

			nLeaves := int64(fx.PieceSize) / proof.NODE_SIZE
			sampleParams := proof.ComputeCacheProofParams(layerIdx, 0)
			challenges := []int64{
				0,                              // first leaf of first section
				sampleParams.LeavesPerNode - 1, // last leaf of first section
				nLeaves / 2,                    // middle of tree
				nLeaves - 1,                    // last leaf of last section
			}

			for _, challenge := range challenges {
				// Call the production function
				cachedProof, err := proof.GenerateCachedProof(
					ctx, reader, cache, pieceCidV2, challenge)
				require.NoError(t, err, "GenerateCachedProof at challenge %d", challenge)
				require.NotNil(t, cachedProof, "expected non-nil proof at challenge %d", challenge)

				// Verify the proof against the externally-anchored root
				ok := proof.VerifyProof(cachedProof.Leaf, cachedProof.Proof, root, uint64(challenge))
				require.True(t, ok,
					"cached proof should verify at challenge %d", challenge)

				// Cross-check: root from GenerateCachedProof matches fixture
				require.Equal(t, root, cachedProof.Root,
					"cached proof root should match fixture at challenge %d", challenge)

				// Cross-check: leaf should match full memtree's leaf
				directProof, err := proof.MemtreeProof(fullMemtree, challenge)
				require.NoError(t, err)
				require.Equal(t, directProof.Leaf, cachedProof.Leaf,
					"leaf mismatch between cached and direct proof at challenge %d", challenge)
			}
		})
	}
}

// TestGenerateCachedProofNilOnMissingCache verifies that GenerateCachedProof
// returns (nil, nil) when no cache is available, signaling fallback.
func TestGenerateCachedProofNilOnMissingCache(t *testing.T) {
	fixtures := loadCommPFixtures(t)
	fx := fixtures[3] // 8 KiB padded, small enough to be fast

	pieceCidV2, err := commcid.DataCommitmentToPieceCidv2(fx.RawCommP[:], uint64(fx.PayloadSize))
	require.NoError(t, err)

	emptyCache := newMemProofCache(0, nil) // no nodes = no cache
	reader := &memPieceReader{data: map[string][]byte{}}

	result, err := proof.GenerateCachedProof(
		context.Background(), reader, emptyCache, pieceCidV2, 0)
	require.NoError(t, err)
	require.Nil(t, result, "should return nil when no cache available")
}
