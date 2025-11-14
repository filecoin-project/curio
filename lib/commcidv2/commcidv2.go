package commcidv2

import (
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
)

func IsPieceCidV2(c cid.Cid) bool {
	if c.Type() != uint64(multicodec.Raw) {
		return false
	}

	decoded, err := multihash.Decode(c.Hash())
	if err != nil {
		return false
	}

	if decoded.Code != uint64(multicodec.Fr32Sha256Trunc254Padbintree) {
		return false
	}

	if len(decoded.Digest) < 34 {
		return false
	}

	return true
}

func IsCidV1PieceCid(c cid.Cid) bool {
	decoded, err := multihash.Decode(c.Hash())
	if err != nil {
		return false
	}

	filCodec := multicodec.Code(c.Type())
	filMh := multicodec.Code(decoded.Code)

	// Check if it's a valid Filecoin commitment type
	switch filCodec {
	case multicodec.FilCommitmentUnsealed:
		if filMh != multicodec.Sha2_256Trunc254Padded {
			return false
		}
	/* case multicodec.FilCommitmentSealed:
	if filMh != multicodec.PoseidonBls12_381A2Fc1 {
		return false
	} */
	default:
		// Neither unsealed nor sealed commitment
		return false
	}

	// Commitments must be exactly 32 bytes
	if len(decoded.Digest) != 32 {
		return false
	}

	return true
}
