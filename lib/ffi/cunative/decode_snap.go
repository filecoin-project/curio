package cunative

/*
#cgo CFLAGS: -I${SRCDIR}/../../../extern/supra_seal/deps/blst/bindings
#cgo LDFLAGS: -L${SRCDIR}/../../../extern/supra_seal/deps/blst -lblst
#include <stdint.h>
#include <stdlib.h>
#include "blst.h"

void snap_decode_loop(const uint8_t *replica, const uint8_t *key, const uint8_t *rho_invs, uint8_t *out, size_t node_count, size_t node_size) {
    blst_fr replica_fr, key_fr, rho_inv_fr, out_fr;

    for (size_t i = 0; i < node_count; i++) {
        // Read replica data
        blst_fr_from_uint64(&replica_fr, (const uint64_t*)(replica + i * node_size));

        // Read key data
        blst_fr_from_uint64(&key_fr, (const uint64_t*)(key + i * node_size));

        // Read rho inverse
        blst_fr_from_uint64(&rho_inv_fr, (const uint64_t*)(rho_invs + i * 32));

        // Perform the decoding operation
        blst_fr_sub(&out_fr, &replica_fr, &key_fr);
        blst_fr_mul(&out_fr, &out_fr, &rho_inv_fr);

        // Write the result
        blst_uint64_from_fr((uint64_t*)(out + i * node_size), &out_fr);
    }
}
*/
import "C"

import (
	"encoding/binary"
	"github.com/filecoin-project/curio/lib/proof"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/triplewz/poseidon"
	ff "github.com/triplewz/poseidon/bls12_381"
	"golang.org/x/xerrors"
	"io"
	"math/big"
	"math/bits"
	"unsafe"
)

type B32le = [32]byte
type BytesLE = []byte

func DecodeSnap(spt abi.RegisteredSealProof, commD, commK cid.Cid, key, replica io.Reader, out io.Writer) error {
	ssize, err := spt.SectorSize()
	if err != nil {
		return xerrors.Errorf("failed to get sector size: %w", err)
	}

	nodesCount := uint64(ssize / proof.NODE_SIZE)

	commDNew, err := commcid.CIDToDataCommitmentV1(commD)
	if err != nil {
		return xerrors.Errorf("failed to convert commD to CID: %w", err)
	}

	commROld, err := commcid.CIDToReplicaCommitmentV1(commK)
	if err != nil {
		return xerrors.Errorf("failed to convert commK to replica commitment: %w", err)
	}

	// Calculate phi
	phi, err := Phi(commDNew, commROld)
	if err != nil {
		return xerrors.Errorf("failed to calculate phi: %w", err)
	}

	// Precompute all rho^-1 values
	h := hDefault(nodesCount)
	rhoInvs, err := NewInv(phi, h, nodesCount)
	if err != nil {
		return xerrors.Errorf("failed to compute rho inverses: %w", err)
	}

	// Allocate buffers
	replicaBuffer := make([]byte, ssize)
	keyBuffer := make([]byte, ssize)
	outBuffer := make([]byte, ssize)

	// Read all data into buffers
	_, err = io.ReadFull(replica, replicaBuffer)
	if err != nil {
		return xerrors.Errorf("failed to read replica data: %w", err)
	}
	_, err = io.ReadFull(key, keyBuffer)
	if err != nil {
		return xerrors.Errorf("failed to read key data: %w", err)
	}

	// Convert rhoInvs to byte slice
	rhoInvsBytes := make([]byte, nodesCount*32)
	for i := uint64(0); i < nodesCount; i++ {
		rhoInv := rhoInvs.Get(i)
		copy(rhoInvsBytes[i*32:(i+1)*32], rhoInv[:])
	}

	// Call the C function
	C.snap_decode_loop(
		(*C.uint8_t)(unsafe.Pointer(&replicaBuffer[0])),
		(*C.uint8_t)(unsafe.Pointer(&keyBuffer[0])),
		(*C.uint8_t)(unsafe.Pointer(&rhoInvsBytes[0])),
		(*C.uint8_t)(unsafe.Pointer(&outBuffer[0])),
		C.size_t(nodesCount),
		C.size_t(proof.NODE_SIZE),
	)

	// Write the result
	_, err = out.Write(outBuffer)
	if err != nil {
		return xerrors.Errorf("failed to write output data: %w", err)
	}

	return nil
}

// Phi implements the phi function as described in the Rust code.
// It computes phi = H(comm_d_new, comm_r_old) using Poseidon hash with a custom domain separation tag.
func Phi(commDNew, commROld BytesLE) ([32]byte, error) {
	inputA := bigIntLE(commDNew)
	inputB := bigIntLE(commROld)
	input := []*big.Int{inputA, inputB}

	// Generate Poseidon constants with a custom domain separation tag
	cons, err := genPoseidonConstantsWithCustomTag(3, 1) // Using 1 as the custom tag
	if err != nil {
		return [32]byte{}, xerrors.Errorf("failed to generate Poseidon constants: %w", err)
	}

	// Compute the hash
	h, err := poseidon.Hash(input, cons, poseidon.OptimizedStatic)
	if err != nil {
		return [32]byte{}, xerrors.Errorf("failed to compute Poseidon hash: %w", err)
	}

	hElement := ffElementBytesLE(new(ff.Element).SetBigInt(h))

	return hElement, nil
}

func rho(phi [32]byte, high uint32) (*ff.Element, error) {
	// Reverse phi for correct endianness
	for i, j := 0, len(phi)-1; i < j; i, j = i+1, j-1 {
		phi[i], phi[j] = phi[j], phi[i]
	}

	inputA := new(big.Int).SetBytes(phi[:])
	inputB := new(big.Int).SetUint64(uint64(high))
	input := []*big.Int{inputA, inputB}

	// Generate Poseidon constants with a custom domain separation tag
	cons, err := genPoseidonConstantsWithCustomTag(3, 1) // Using 1 as the custom tag
	if err != nil {
		return nil, err
	}

	// Compute the hash
	h, err := poseidon.Hash(input, cons, poseidon.OptimizedStatic)
	if err != nil {
		return nil, err
	}

	return new(ff.Element).SetBigInt(h), nil
}

// Rhos represents a collection of precomputed rho values
type Rhos struct {
	rhos    map[uint64]B32le
	bitsShr uint64
}

// NewInv generates the inverted rhos for a certain number of nodes
func NewInv(phi [32]byte, h uint64, nodesCount uint64) (*Rhos, error) {
	return NewInvRange(phi, h, nodesCount, 0, nodesCount)
}

// NewInvRange generates the inverted rhos for a certain number of nodes and range
func NewInvRange(phi [32]byte, h uint64, nodesCount, offset, num uint64) (*Rhos, error) {
	bitsShr := calcBitsShr(h, nodesCount)
	highRange := calcHighRange(offset, num, bitsShr)

	rhos := make(map[uint64]B32le)
	for high := highRange.Start; high <= highRange.End; high++ {
		rhoVal, err := rho(phi, uint32(high))
		if err != nil {
			return nil, err
		}

		invRho := new(ff.Element).Inverse(rhoVal) // same as blst_fr_eucl_inverse??
		rhos[high] = ffElementBytesLE(invRho)
	}

	return &Rhos{
		rhos:    rhos,
		bitsShr: bitsShr,
	}, nil
}

// Get retrieves the rho for a specific node offset
func (r *Rhos) Get(offset uint64) B32le {
	high := offset >> r.bitsShr
	return r.rhos[high]
}

func calcBitsShr(h uint64, nodesCount uint64) uint64 {
	nodeIndexBitLen := 64 - uint64(bits.TrailingZeros64(nodesCount))
	return nodeIndexBitLen - h
}

type Range struct {
	Start, End uint64
}

func calcHighRange(offset, num uint64, bitsShr uint64) Range {
	firstHigh := offset >> bitsShr
	lastHigh := (offset + num - 1) >> bitsShr
	return Range{Start: firstHigh, End: lastHigh}
}

// genPoseidonConstantsWithCustomTag generates Poseidon constants with a custom domain separation tag.
func genPoseidonConstantsWithCustomTag(width, customTag int) (*poseidon.PoseidonConst, error) {
	cons, err := poseidon.GenPoseidonConstants(width)
	if err != nil {
		return nil, err
	}

	// Set the custom domain tag
	cons.RoundConsts[0] = new(ff.Element).SetUint64(uint64(customTag))

	return cons, nil
}

// the `h` values allowed for the given sector-size. Each `h` value is a possible number
// of high bits taken from each challenge `c`. A single value of `h = hs[i]` is taken from `hs`
// for each proof; the circuit takes `h_select = 2^i` as a public input.
//
// Those values are hard-coded for the circuit and cannot be changed without another trusted
// setup.
//
// Returns the `h` for the given sector-size. The `h` value is the number of high bits taken from
// each challenge `c`. For production use, it was determined to use the value at index 3, which
// translates to a value of 10 for production sector sizes.
func hDefault(nodesCount uint64) uint64 {
	const nodes32KiB = 32 * 1024 / proof.NODE_SIZE
	if nodesCount <= nodes32KiB {
		return 1
	}
	return 10
}

func ffElementBytesLE(z *ff.Element) (res B32le) {
	_z := z.ToRegular()
	binary.LittleEndian.PutUint64(res[0:8], _z[0])
	binary.LittleEndian.PutUint64(res[8:16], _z[1])
	binary.LittleEndian.PutUint64(res[16:24], _z[2])
	binary.LittleEndian.PutUint64(res[24:32], _z[3])

	return
}

// BE new(big.Int).SetBytes(commDNew[:])
func bigIntLE(b []byte) *big.Int {
	// invert b
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	return new(big.Int).SetBytes(b)
}
