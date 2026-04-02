package pdp

import (
	"fmt"
	"math/bits"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"

	commcid "github.com/filecoin-project/go-fil-commcid"
)

// PieceCidInfo holds all derived information from a piece CID.
// Depending on how it was constructed, some fields may be zero values.
type PieceCidInfo struct {
	CidV1   cid.Cid // Always populated
	CidV2   cid.Cid // Undef if parsed from v1 without size
	RawSize uint64  // 0 if parsed from v1 without size
}

// HasV2 returns true if the CidV2 is available (not Undef).
func (p *PieceCidInfo) HasV2() bool {
	return p.CidV2.Defined()
}

// ParsePieceCid parses any valid piece CID string (v1 or v2).
// If the input is v1, CidV2 will be cid.Undef and RawSize will be 0.
// If the input is v2, all fields will be populated.
func ParsePieceCid(cidStr string) (*PieceCidInfo, error) {
	pieceCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode piece CID: %w", err)
	}

	switch pieceCid.Prefix().MhType {
	case uint64(multicodec.Sha2_256Trunc254Padded):
		// v1 CID - we don't have size info
		return &PieceCidInfo{
			CidV1:   pieceCid,
			CidV2:   cid.Undef,
			RawSize: 0,
		}, nil

	case uint64(multicodec.Fr32Sha256Trunc254Padbintree):
		// v2 CID - extract v1 and size
		v1, rawSize, err := commcid.PieceCidV1FromV2(pieceCid)
		if err != nil {
			return nil, fmt.Errorf("failed to extract v1 from v2: %w", err)
		}
		return &PieceCidInfo{
			CidV1:   v1,
			CidV2:   pieceCid,
			RawSize: rawSize,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported piece CID type: %d", pieceCid.Prefix().MhType)
	}
}

// ParsePieceCidV2 parses a piece CID string that must be in v2 format.
// Returns an error if the input is a v1 CID.
// All fields in the returned PieceCidInfo will be populated.
func ParsePieceCidV2(cidStr string) (*PieceCidInfo, error) {
	pieceCid, err := cid.Decode(cidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CID: %w", err)
	}

	if pieceCid.Prefix().MhType != uint64(multicodec.Fr32Sha256Trunc254Padbintree) {
		return nil, fmt.Errorf("piece CID must be PieceCIDv2 format (got multihash type %d)", pieceCid.Prefix().MhType)
	}

	v1, rawSize, err := commcid.PieceCidV1FromV2(pieceCid)
	if err != nil {
		return nil, fmt.Errorf("failed to extract v1 from v2: %w", err)
	}

	return &PieceCidInfo{
		CidV1:   v1,
		CidV2:   pieceCid,
		RawSize: rawSize,
	}, nil
}

// PieceCidV2FromV1 constructs a complete PieceCidInfo from a v1 CID and raw size.
// This is typically used when reading from the database (which stores v1 + size)
// and needing to return v2 in API responses.
func PieceCidV2FromV1(v1 cid.Cid, rawSize uint64) (*PieceCidInfo, error) {
	if rawSize == 0 {
		return nil, fmt.Errorf("rawSize must be provided to construct v2 from v1")
	}

	v2, err := commcid.PieceCidV2FromV1(v1, rawSize)
	if err != nil {
		return nil, fmt.Errorf("failed to construct v2 from v1: %w", err)
	}

	return &PieceCidInfo{
		CidV1:   v1,
		CidV2:   v2,
		RawSize: rawSize,
	}, nil
}

// PieceCidV2FromV1Str is a convenience function that parses a v1 CID string
// and constructs a complete PieceCidInfo. This handles the common case of
// reading a v1 CID string from the database along with its size.
func PieceCidV2FromV1Str(v1Str string, rawSize uint64) (*PieceCidInfo, error) {
	v1, err := cid.Decode(v1Str)
	if err != nil {
		return nil, fmt.Errorf("failed to decode v1 CID: %w", err)
	}

	// Verify it's actually a v1 CID
	if v1.Prefix().MhType != uint64(multicodec.Sha2_256Trunc254Padded) {
		return nil, fmt.Errorf("expected v1 CID but got multihash type %d", v1.Prefix().MhType)
	}

	return PieceCidV2FromV1(v1, rawSize)
}

// PadPieceSize calculates the padded piece size from raw size using FR32 padding.
// FR32 encoding: 127 bytes of data become 128 bytes, then rounded up to next power of 2.
func PadPieceSize(rawSize int64) int64 {
	if rawSize == 0 {
		return 0
	}
	// Apply FR32 ratio: ceil(rawSize * 128 / 127)
	fr32Padded := (uint64(rawSize)*128 + 126) / 127
	// Round up to next power of 2
	if fr32Padded == 0 {
		return 1
	}
	fr32Padded--
	return int64(1 << bits.Len64(fr32Padded))
}
