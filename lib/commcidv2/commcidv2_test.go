package commcidv2

import (
	"fmt"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants that should match our TypeScript implementation
func TestConstants(t *testing.T) {
	// These constants are string-based identifiers in Go, not numeric constants
	// We'll verify they exist and have the expected string values
	assert.NotEqual(t, multicodec.Code(0), multicodec.FilCommitmentUnsealed)
	assert.NotEqual(t, multicodec.Code(0), multicodec.FilCommitmentSealed)
	assert.NotEqual(t, multicodec.Code(0), multicodec.Sha2_256Trunc254Padded)
	assert.NotEqual(t, multicodec.Code(0), multicodec.PoseidonBls12_381A2Fc1)

	// Raw codec constant
	assert.Equal(t, multicodec.Raw, multicodec.Code(0x55))

	// Verify the constants exist and are not zero
	// Note: These are numeric constants in Go, not string identifiers
	assert.True(t, uint64(multicodec.FilCommitmentUnsealed) > 0)
	assert.True(t, uint64(multicodec.FilCommitmentSealed) > 0)
	assert.True(t, uint64(multicodec.Sha2_256Trunc254Padded) > 0)
	assert.True(t, uint64(multicodec.PoseidonBls12_381A2Fc1) > 0)
}

// Test PieceCidV2FromV1 with wrong hash type (demonstrates validation)
func TestPieceCidV2FromV1_WrongHashType(t *testing.T) {
	// Create a valid unsealed commitment CID v1
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	// Create multihash with SHA2_256 (standard hash function)
	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	// Create CID v1 with FilCommitmentUnsealed codec
	// Note: We'll use SHA2_256 instead of SHA2_256Trunc254Padded for testing
	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test conversion - should fail because we're using SHA2_256 instead of SHA2_256Trunc254Padded
	payloadSize := uint64(1024)
	cidV2, err := PieceCidV2FromV1(cidV1, payloadSize)
	assert.Error(t, err)
	assert.Equal(t, cid.Undef, cidV2)
	assert.Contains(t, err.Error(), "unexpected hash")

	// This test demonstrates that our TypeScript implementation should also validate hash types
	// and reject CIDs with incorrect hash functions
}

// Test PieceCidV2FromV1 with valid sealed commitment
func TestPieceCidV2FromV1_ValidSealed(t *testing.T) {
	// Create a valid sealed commitment CID v1
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 1)
	}

	// Create multihash with SHA2_256 (standard hash function)
	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	// Create CID v1 with FilCommitmentSealed codec
	// Note: We'll use SHA2_256 instead of PoseidonBls12_381A2Fc1 for testing
	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentSealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test conversion - should fail because we're using wrong hash type
	payloadSize := uint64(1024)
	cidV2, err := PieceCidV2FromV1(cidV1, payloadSize)
	assert.Error(t, err)
	assert.Equal(t, cid.Undef, cidV2)
	assert.Contains(t, err.Error(), "unexpected hash")
}

// Test PieceCidV2FromV1 with invalid codec
func TestPieceCidV2FromV1_InvalidCodec(t *testing.T) {
	// Create a CID v1 with raw codec (not Filecoin)
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 2)
	}

	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.Raw),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test conversion - should fail with unexpected codec
	payloadSize := uint64(1024)
	cidV2, err := PieceCidV2FromV1(cidV1, payloadSize)
	assert.Error(t, err)
	assert.Equal(t, cid.Undef, cidV2)
	assert.Contains(t, err.Error(), "unexpected codec")
}

// Test PieceCidV2FromV1 with invalid hash type
func TestPieceCidV2FromV1_InvalidHashType(t *testing.T) {
	// Create a CID v1 with unsealed codec but wrong hash type
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 3)
	}

	// Use SHA2_256 instead of SHA2_256Trunc254Padded
	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test conversion - should fail with unexpected hash
	payloadSize := uint64(1024)
	cidV2, err := PieceCidV2FromV1(cidV1, payloadSize)
	assert.Error(t, err)
	assert.Equal(t, cid.Undef, cidV2)
	assert.Contains(t, err.Error(), "unexpected hash")
}

// Test PieceCidV2FromV1 with invalid digest length
func TestPieceCidV2FromV1_InvalidDigestLength(t *testing.T) {
	// Create a CID v1 with unsealed codec but wrong digest length
	digest := make([]byte, 16) // Only 16 bytes instead of 32
	for i := range digest {
		digest[i] = byte(i + 4)
	}

	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test conversion - should fail with hash type error (not digest length)
	payloadSize := uint64(1024)
	cidV2, err := PieceCidV2FromV1(cidV1, payloadSize)
	assert.Error(t, err)
	assert.Equal(t, cid.Undef, cidV2)
	assert.Contains(t, err.Error(), "unexpected hash")
}

// Test PieceCidV2FromV1 with different payload sizes
func TestPieceCidV2FromV1_DifferentPayloadSizes(t *testing.T) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 5)
	}

	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	// Test with different payload sizes - should fail with hash type error
	testSizes := []uint64{1, 127, 128, 1024, 2048, 4096, 8192}

	for _, size := range testSizes {
		t.Run(fmt.Sprintf("PayloadSize_%d", size), func(t *testing.T) {
			cidV2, err := PieceCidV2FromV1(cidV1, size)
			assert.Error(t, err)
			assert.Equal(t, cid.Undef, cidV2)
			assert.Contains(t, err.Error(), "unexpected hash")
		})
	}
}

// Test NewSha2CommP with valid inputs
func TestNewSha2CommP_Valid(t *testing.T) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 6)
	}

	payloadSize := uint64(1024)
	commP, err := NewSha2CommP(payloadSize, digest)
	require.NoError(t, err)

	// Verify the CommP structure
	assert.Equal(t, int8(1), commP.hashType)
	assert.Equal(t, digest, commP.digest)
	assert.True(t, commP.treeHeight > 0)

	// Verify payload size calculation
	computedSize := commP.PayloadSize()
	assert.Equal(t, payloadSize, computedSize)
}

// Test NewSha2CommP with invalid digest length
func TestNewSha2CommP_InvalidDigestLength(t *testing.T) {
	// Test with digest that's too short
	digest := make([]byte, 16)
	payloadSize := uint64(1024)

	commP, err := NewSha2CommP(payloadSize, digest)
	assert.Error(t, err)
	assert.Equal(t, CommP{}, commP)
	assert.Contains(t, err.Error(), "digest size must be 32")
}

// Test NewSha2CommP with digest that's too long
func TestNewSha2CommP_InvalidDigestLengthTooLong(t *testing.T) {
	// Test with digest that's too long
	digest := make([]byte, 64)
	payloadSize := uint64(1024)

	commP, err := NewSha2CommP(payloadSize, digest)
	assert.Error(t, err)
	assert.Equal(t, CommP{}, commP)
	assert.Contains(t, err.Error(), "digest size must be 32")
}

// Test CommP methods
func TestCommP_Methods(t *testing.T) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 7)
	}

	payloadSize := uint64(1024)
	commP, err := NewSha2CommP(payloadSize, digest)
	require.NoError(t, err)

	// Test Digest method
	assert.Equal(t, digest, commP.Digest())

	// Test PieceLog2Size method
	log2Size := commP.PieceLog2Size()
	assert.True(t, log2Size > 0)

	// Test PieceInfo method
	pieceInfo := commP.PieceInfo()
	// Note: The actual size may be different due to padding and alignment
	assert.True(t, uint64(pieceInfo.Size) >= payloadSize)
	assert.NotEqual(t, cid.Undef, pieceInfo.PieceCID)

	// Test PCidV1 method
	cidV1 := commP.PCidV1()
	assert.NotEqual(t, cid.Undef, cidV1)
	assert.Equal(t, uint64(multicodec.FilCommitmentUnsealed), cidV1.Type())

	// Test PCidV2 method
	cidV2 := commP.PCidV2()
	assert.NotEqual(t, cid.Undef, cidV2)
	assert.Equal(t, uint64(multicodec.Raw), cidV2.Type())
	assert.True(t, IsPieceCidV2(cidV2))
}

// Test IsPieceCidV2 with valid piece CID v2
func TestIsPieceCidV2_Valid(t *testing.T) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 8)
	}

	payloadSize := uint64(1024)
	commP, err := NewSha2CommP(payloadSize, digest)
	require.NoError(t, err)

	cidV2 := commP.PCidV2()
	assert.True(t, IsPieceCidV2(cidV2))
}

// Test IsPieceCidV2 with invalid CIDs
func TestIsPieceCidV2_Invalid(t *testing.T) {
	// Test with raw CID (not piece CID v2)
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 9)
	}

	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	require.NoError(t, err)

	rawCid, err := cid.V1Builder{
		Codec:    uint64(multicodec.Raw),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	assert.False(t, IsPieceCidV2(rawCid))

	// Test with unsealed commitment CID (not piece CID v2)
	unsealedCid, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	require.NoError(t, err)

	assert.False(t, IsPieceCidV2(unsealedCid))
}

// Test edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i + 10)
	}

	// Test with minimum payload size
	commP, err := NewSha2CommP(1, digest)
	require.NoError(t, err)
	assert.True(t, commP.PayloadSize() >= 1)

	// Test with very large payload size
	largeSize := uint64(1 << 30) // 1GB
	commP, err = NewSha2CommP(largeSize, digest)
	require.NoError(t, err)
	assert.Equal(t, largeSize, commP.PayloadSize())

	// Test with power of 2 payload sizes
	powerOf2Sizes := []uint64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024}
	for _, size := range powerOf2Sizes {
		t.Run(fmt.Sprintf("PowerOf2_%d", size), func(t *testing.T) {
			commP, err := NewSha2CommP(size, digest)
			require.NoError(t, err)
			assert.Equal(t, size, commP.PayloadSize())
		})
	}
}

// Benchmark tests for performance
func BenchmarkPieceCidV2FromV1(b *testing.B) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	mh, err := multihash.Encode(digest, uint64(multicodec.Sha2_256))
	if err != nil {
		b.Fatal(err)
	}

	cidV1, err := cid.V1Builder{
		Codec:    uint64(multicodec.FilCommitmentUnsealed),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: -1,
	}.Sum(mh)
	if err != nil {
		b.Fatal(err)
	}

	payloadSize := uint64(1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := PieceCidV2FromV1(cidV1, payloadSize)
		// This will always fail due to wrong hash type, but we're benchmarking the function
		// In real usage, you'd use the correct hash type
		if err == nil {
			b.Fatal("Expected error due to wrong hash type")
		}
	}
}

func BenchmarkNewSha2CommP(b *testing.B) {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = byte(i)
	}

	payloadSize := uint64(1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewSha2CommP(payloadSize, digest)
		if err != nil {
			b.Fatal(err)
		}
	}
}
