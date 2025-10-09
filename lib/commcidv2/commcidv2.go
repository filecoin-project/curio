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
