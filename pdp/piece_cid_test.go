package pdp

import (
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

// Test CIDs - these are real valid CIDs from the codebase
const (
	// PieceCIDv2 format (Fr32Sha256Trunc254Padbintree multihash)
	testV2Cid1 = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	testV2Cid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"

	// PieceCIDv1 format (Sha2_256Trunc254Padded multihash)
	testV1Cid1 = "baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy"
)

func TestParsePieceCid(t *testing.T) {
	t.Run("parses v2 CID correctly", func(t *testing.T) {
		info, err := ParsePieceCid(testV2Cid1)
		require.NoError(t, err)
		require.NotNil(t, info)

		// V1 should be populated
		require.True(t, info.CidV1.Defined())
		require.NotEqual(t, testV2Cid1, info.CidV1.String(), "v1 should differ from v2")

		// V2 should be the original
		require.True(t, info.HasV2())
		require.Equal(t, testV2Cid1, info.CidV2.String())

		// RawSize should be populated
		require.NotZero(t, info.RawSize)
	})

	t.Run("parses v1 CID correctly", func(t *testing.T) {
		info, err := ParsePieceCid(testV1Cid1)
		require.NoError(t, err)
		require.NotNil(t, info)

		// V1 should be the original
		require.True(t, info.CidV1.Defined())
		require.Equal(t, testV1Cid1, info.CidV1.String())

		// V2 should be Undef (no size info available)
		require.False(t, info.HasV2())
		require.Equal(t, cid.Undef, info.CidV2)

		// RawSize should be 0 (no size info available)
		require.Zero(t, info.RawSize)
	})

	t.Run("rejects invalid CID", func(t *testing.T) {
		_, err := ParsePieceCid("not-a-cid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to decode")
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := ParsePieceCid("")
		require.Error(t, err)
	})

	t.Run("rejects non-piece CID", func(t *testing.T) {
		// A regular content CID (not a piece CID)
		_, err := ParsePieceCid("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported piece CID type")
	})
}

func TestParsePieceCidV2(t *testing.T) {
	t.Run("parses v2 CID correctly", func(t *testing.T) {
		info, err := ParsePieceCidV2(testV2Cid1)
		require.NoError(t, err)
		require.NotNil(t, info)

		// All fields should be populated
		require.True(t, info.CidV1.Defined())
		require.True(t, info.HasV2())
		require.Equal(t, testV2Cid1, info.CidV2.String())
		require.NotZero(t, info.RawSize)
	})

	t.Run("rejects v1 CID", func(t *testing.T) {
		_, err := ParsePieceCidV2(testV1Cid1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "must be PieceCIDv2")
	})

	t.Run("rejects invalid CID", func(t *testing.T) {
		_, err := ParsePieceCidV2("not-a-cid")
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid CID")
	})

	t.Run("multiple v2 CIDs produce different v1s", func(t *testing.T) {
		info1, err := ParsePieceCidV2(testV2Cid1)
		require.NoError(t, err)

		info2, err := ParsePieceCidV2(testV2Cid2)
		require.NoError(t, err)

		require.NotEqual(t, info1.CidV1.String(), info2.CidV1.String())
		require.NotEqual(t, info1.CidV2.String(), info2.CidV2.String())
	})
}

func TestPieceCidV2FromV1(t *testing.T) {
	// First get a valid v1 CID and its raw size from a v2
	v2Info, err := ParsePieceCidV2(testV2Cid1)
	require.NoError(t, err)

	t.Run("reconstructs v2 from v1 and size", func(t *testing.T) {
		info, err := PieceCidV2FromV1(v2Info.CidV1, v2Info.RawSize)
		require.NoError(t, err)
		require.NotNil(t, info)

		// Should reconstruct to the same values
		require.Equal(t, v2Info.CidV1.String(), info.CidV1.String())
		require.Equal(t, v2Info.CidV2.String(), info.CidV2.String())
		require.Equal(t, v2Info.RawSize, info.RawSize)
	})

	t.Run("rejects zero size", func(t *testing.T) {
		_, err := PieceCidV2FromV1(v2Info.CidV1, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "rawSize must be provided")
	})
}

func TestPieceCidV2FromV1Str(t *testing.T) {
	// First get a valid v1 CID string and its raw size from a v2
	v2Info, err := ParsePieceCidV2(testV2Cid1)
	require.NoError(t, err)
	v1Str := v2Info.CidV1.String()

	t.Run("reconstructs v2 from v1 string and size", func(t *testing.T) {
		info, err := PieceCidV2FromV1Str(v1Str, v2Info.RawSize)
		require.NoError(t, err)
		require.NotNil(t, info)

		// Should reconstruct to the same values
		require.Equal(t, v1Str, info.CidV1.String())
		require.Equal(t, testV2Cid1, info.CidV2.String())
		require.Equal(t, v2Info.RawSize, info.RawSize)
	})

	t.Run("rejects v2 CID string", func(t *testing.T) {
		_, err := PieceCidV2FromV1Str(testV2Cid1, 1024)
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected v1 CID")
	})

	t.Run("rejects invalid CID string", func(t *testing.T) {
		_, err := PieceCidV2FromV1Str("not-a-cid", 1024)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to decode")
	})

	t.Run("rejects zero size", func(t *testing.T) {
		_, err := PieceCidV2FromV1Str(v1Str, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "rawSize must be provided")
	})
}

func TestRoundTrip(t *testing.T) {
	t.Run("v2 -> v1 -> v2 round trip", func(t *testing.T) {
		// Start with v2
		original, err := ParsePieceCidV2(testV2Cid1)
		require.NoError(t, err)

		// Simulate storing v1 + size in DB, then reconstructing v2
		reconstructed, err := PieceCidV2FromV1(original.CidV1, original.RawSize)
		require.NoError(t, err)

		// Should match original
		require.Equal(t, original.CidV1.String(), reconstructed.CidV1.String())
		require.Equal(t, original.CidV2.String(), reconstructed.CidV2.String())
		require.Equal(t, original.RawSize, reconstructed.RawSize)
	})

	t.Run("v2 string -> v1 string -> v2 string round trip", func(t *testing.T) {
		// Start with v2 string
		info1, err := ParsePieceCidV2(testV2Cid1)
		require.NoError(t, err)

		// Get v1 string (as would be stored in DB)
		v1Str := info1.CidV1.String()
		rawSize := info1.RawSize

		// Reconstruct v2 (as would be done for API response)
		info2, err := PieceCidV2FromV1Str(v1Str, rawSize)
		require.NoError(t, err)

		// Should get back original v2 string
		require.Equal(t, testV2Cid1, info2.CidV2.String())
	})
}

func TestHasV2(t *testing.T) {
	t.Run("returns true for v2 input", func(t *testing.T) {
		info, err := ParsePieceCid(testV2Cid1)
		require.NoError(t, err)
		require.True(t, info.HasV2())
	})

	t.Run("returns false for v1 input", func(t *testing.T) {
		info, err := ParsePieceCid(testV1Cid1)
		require.NoError(t, err)
		require.False(t, info.HasV2())
	})

	t.Run("returns true for reconstructed v2", func(t *testing.T) {
		v2Info, err := ParsePieceCidV2(testV2Cid1)
		require.NoError(t, err)

		info, err := PieceCidV2FromV1(v2Info.CidV1, v2Info.RawSize)
		require.NoError(t, err)
		require.True(t, info.HasV2())
	})
}

func TestParsePieceCidV2_ErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "invalid CID string",
			input:       "not-a-cid",
			errContains: "invalid CID",
		},
		{
			name:        "v1 CID rejected",
			input:       testV1Cid1,
			errContains: "must be PieceCIDv2",
		},
		{
			name:        "empty string",
			input:       "",
			errContains: "invalid CID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePieceCidV2(tt.input)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tt.errContains),
				"error %q should contain %q", err.Error(), tt.errContains)
		})
	}
}
