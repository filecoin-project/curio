package commcidv2

import (
	"math/bits"

	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/multiformats/go-varint"
	"golang.org/x/xerrors"

	filabi "github.com/filecoin-project/go-state-types/abi"
)

type CommP struct {
	hashType       int8
	treeHeight     int8
	payloadPadding uint64
	digest         []byte
}

// hardcoded for npw
const (
	nodeSize     = 32
	nodeLog2Size = 5
)

var mhMeta = map[int8]struct {
	treeArity    int8
	nodeLog2Size int8
	pCidV1Pref   string
	pCidV2Pref   string
}{
	1: {
		treeArity:    2,
		nodeLog2Size: nodeLog2Size,
		pCidV1Pref:   "\x01" + "\x81\xE2\x03" + "\x92\x20" + "\x20", // + 32 byte digest == total 39 byte cid
		pCidV2Pref:   "\x01" + "\x55" + "\x91\x20",                  // + mh varlen + varpad + int8 height + 32 byte digest == total AT LEAST 39 byte cid
	},
}

func CommPFromPieceInfo(pi filabi.PieceInfo) (CommP, error) {
	var cp CommP
	if bits.OnesCount64(uint64(pi.Size)) > 1 {
		return cp, xerrors.Errorf("malformed PieceInfo: .Size %d not a power of 2", pi.Size)
	}

	// hardcoded until we get another commitment type
	cp.hashType = 1
	ks := pi.PieceCID.KeyString()
	cp.digest = []byte(ks[len(ks)-nodeSize:])

	cp.treeHeight = 63 - int8(bits.LeadingZeros64(uint64(pi.Size))) - nodeLog2Size

	return cp, nil
}

func CommPFromPCidV2(c cid.Cid) (CommP, error) {
	var cp CommP

	dmh, err := multihash.Decode(c.Hash())
	if err != nil {
		return cp, xerrors.Errorf("decoding cid: %w", err)
	}

	// hardcoded for now at https://github.com/multiformats/multicodec/pull/331/files#diff-bf5b449ed8c1850371f42808a186b5c5089edd0025700505a6b8f426cd54a6e4R149
	if dmh.Code != 0x1011 {
		return cp, xerrors.Errorf("unexpected multihash code %d", dmh.Code)
	}

	p, n, err := varint.FromUvarint(dmh.Digest)
	if err != nil {
		return cp, xerrors.Errorf("decoding varint: %w", err)
	}

	cp.hashType = 1
	cp.payloadPadding = p
	cp.treeHeight = int8(dmh.Digest[n])
	cp.digest = dmh.Digest[n+1:]

	return cp, nil
}

func NewSha2CommP(payloadSize uint64, digest []byte) (CommP, error) {
	var cp CommP

	// hardcoded for now
	if len(digest) != nodeSize {
		return cp, xerrors.Errorf("digest size must be 32, got %d", len(digest))
	}

	psz := payloadSize

	// always 4 nodes long
	if psz < 127 {
		psz = 127
	}

	// fr32 expansion, count 127 blocks, rounded up
	boxSize := ((psz + 126) / 127) * 128

	// hardcoded for now
	cp.hashType = 1
	cp.digest = digest

	cp.treeHeight = 63 - int8(bits.LeadingZeros64(boxSize)) - nodeLog2Size
	if bits.OnesCount64(boxSize) != 1 {
		cp.treeHeight++
	}
	cp.payloadPadding = ((1 << (cp.treeHeight - 2)) * 127) - payloadSize

	return cp, nil
}

func (cp *CommP) PayloadSize() uint64 {
	return (1<<(cp.treeHeight-2))*127 - cp.payloadPadding
}

func (cp *CommP) PieceLog2Size() int8 {
	return cp.treeHeight + nodeLog2Size
}

func (cp *CommP) PieceInfo() filabi.PieceInfo {
	return filabi.PieceInfo{
		Size:     filabi.PaddedPieceSize(1 << (cp.treeHeight + nodeLog2Size)),
		PieceCID: cp.PCidV1(), // for now it won't understand anything else but V1... I think
	}
}

func (cp *CommP) PCidV1() cid.Cid {
	pref := mhMeta[cp.hashType].pCidV1Pref
	buf := pool.Get(len(pref) + len(cp.digest))
	copy(buf, pref)
	copy(buf[len(pref):], cp.digest)
	c, err := cid.Cast(buf)
	pool.Put(buf)
	if err != nil {
		panic(err)
	}
	return c
}

func (cp *CommP) PCidV2() cid.Cid {
	pref := mhMeta[cp.hashType].pCidV2Pref

	ps := varint.UvarintSize(cp.payloadPadding)

	buf := pool.Get(len(pref) +
		1 + // size of the entire mh "payload" won't exceed 127 bytes
		ps +
		1 + // the height is an int8
		nodeSize, // digest size, hardcoded for now
	)

	n := copy(buf, pref)
	buf[n] = byte(ps + 1 + nodeSize)
	n++

	n += varint.PutUvarint(buf[n:], cp.payloadPadding)

	buf[n] = byte(cp.treeHeight)
	n++

	copy(buf[n:], cp.digest)

	c, err := cid.Cast(buf)

	pool.Put(buf)
	if err != nil {
		panic(err)
	}

	return c
}

func (cp *CommP) Digest() []byte { return cp.digest }

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
func PieceCidV2FromV1(v1PieceCid cid.Cid, payloadsize uint64) (cid.Cid, error) {
	decoded, err := multihash.Decode(v1PieceCid.Hash())
	if err != nil {
		return cid.Undef, xerrors.Errorf("Error decoding data commitment hash: %w", err)
	}

	filCodec := multicodec.Code(v1PieceCid.Type())
	filMh := multicodec.Code(decoded.Code)

	switch filCodec {
	case multicodec.FilCommitmentUnsealed:
		if filMh != multicodec.Sha2_256Trunc254Padded {
			return cid.Undef, xerrors.Errorf("unexpected hash: %d", filMh)
		}
	case multicodec.FilCommitmentSealed:
		if filMh != multicodec.PoseidonBls12_381A2Fc1 {
			return cid.Undef, xerrors.Errorf("unexpected hash: %d", filMh)
		}
	default: // neither of the codecs above: we are not in Fil teritory
		return cid.Undef, xerrors.Errorf("unexpected codec: %d", filCodec)
	}

	if len(decoded.Digest) != 32 {
		return cid.Undef, xerrors.Errorf("commitments must be 32 bytes long")
	}
	if filCodec != multicodec.FilCommitmentUnsealed {
		return cid.Undef, xerrors.Errorf("unexpected codec: %d", filCodec)
	}

	c, err := NewSha2CommP(payloadsize, decoded.Digest)
	if err != nil {
		return cid.Undef, xerrors.Errorf("error creating CommP: %w", err)
	}

	return c.PCidV2(), nil
}
