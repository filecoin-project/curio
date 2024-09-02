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
	"encoding/hex"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/filecoin-project/curio/lib/proof"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"github.com/snadrus/must"
	"github.com/triplewz/poseidon"
	"golang.org/x/xerrors"
	"io"
	"log"
	"math/big"
	"math/bits"
	"unsafe"
)

type B32le = [32]byte
type BytesLE = []byte

func DecodeSnap(spt abi.RegisteredSealProof, commD, commK cid.Cid, key, replica io.Reader, out io.Writer) error {
	log.Println("Starting DecodeSnap")
	log.Printf("spt: %v, commD: %s, commK: %s", spt, commD, commK)

	ssize, err := spt.SectorSize()
	if err != nil {
		return xerrors.Errorf("failed to get sector size: %w", err)
	}
	log.Printf("Sector size: %d", ssize)

	nodesCount := uint64(ssize / proof.NODE_SIZE)
	log.Printf("Nodes count: %d", nodesCount)

	commDNew, err := commcid.CIDToDataCommitmentV1(commD)
	if err != nil {
		return xerrors.Errorf("failed to convert commD to CID: %w", err)
	}
	log.Printf("commDNew: %x", commDNew)

	commROld, err := commcid.CIDToReplicaCommitmentV1(commK)
	if err != nil {
		return xerrors.Errorf("failed to convert commK to replica commitment: %w", err)
	}
	log.Printf("commROld: %x", commROld)

	// Calculate phi
	phi, err := Phi(commDNew, commROld)
	if err != nil {
		return xerrors.Errorf("failed to calculate phi: %w", err)
	}
	log.Printf("phi: %x", phi)

	// Precompute all rho^-1 values
	h := hDefault(nodesCount)
	log.Printf("h value: %d", h)
	rhoInvs, err := NewInv(phi, h, nodesCount)
	if err != nil {
		return xerrors.Errorf("failed to compute rho inverses: %w", err)
	}
	log.Println("rho inverses computed")

	// Allocate buffers
	replicaBuffer := make([]byte, ssize)
	keyBuffer := make([]byte, ssize)
	outBuffer := make([]byte, ssize)
	log.Printf("Buffers allocated, size: %d", ssize)

	// Read all data into buffers
	_, err = io.ReadFull(replica, replicaBuffer)
	if err != nil {
		return xerrors.Errorf("failed to read replica data: %w", err)
	}
	log.Printf("Replica data read, first 32 bytes: %x", replicaBuffer[:32])

	_, err = io.ReadFull(key, keyBuffer)
	if err != nil {
		return xerrors.Errorf("failed to read key data: %w", err)
	}
	log.Printf("Key data read, first 32 bytes: %x", keyBuffer[:32])

	// Convert rhoInvs to byte slice
	rhoInvsBytes := make([]byte, nodesCount*32)
	for i := uint64(0); i < nodesCount; i++ {
		rhoInv := rhoInvs.Get(i)
		copy(rhoInvsBytes[i*32:(i+1)*32], rhoInv[:])
		if i < 5 {
			log.Printf("rhoInv[%d]: %x", i, rhoInv)
		}
	}
	log.Println("rhoInvs converted to byte slice")

	// Call the C function
	log.Println("Calling snap_decode_loop")
	C.snap_decode_loop(
		(*C.uint8_t)(unsafe.Pointer(&replicaBuffer[0])),
		(*C.uint8_t)(unsafe.Pointer(&keyBuffer[0])),
		(*C.uint8_t)(unsafe.Pointer(&rhoInvsBytes[0])),
		(*C.uint8_t)(unsafe.Pointer(&outBuffer[0])),
		C.size_t(nodesCount),
		C.size_t(proof.NODE_SIZE),
	)
	log.Println("snap_decode_loop completed")

	// Write the result
	log.Printf("Writing output, first 32 bytes: %x", outBuffer[:32])
	_, err = out.Write(outBuffer)
	if err != nil {
		return xerrors.Errorf("failed to write output data: %w", err)
	}
	log.Println("Output written successfully")

	return nil
}

// Phi implements the phi function as described in the Rust code.
// It computes phi = H(comm_d_new, comm_r_old) using Poseidon hash with a custom domain separation tag.
func Phi(commDNew, commROld BytesLE) (B32le, error) {
	inputA := bigIntLE(commDNew)
	inputB := bigIntLE(commROld)
	input := []*big.Int{inputA, inputB}

	cons, err := poseidon.GenPoseidonConstants[*CursedPoseidonGenRandomnessElement](3)
	if err != nil {
		return [32]byte{}, err
	}

	// Compute the hash
	h, err := poseidon.Hash(input, cons, poseidon.OptimizedStatic)
	if err != nil {
		return [32]byte{}, xerrors.Errorf("failed to compute Poseidon hash: %w", err)
	}

	hElement := ffElementBytesLE(new(fr.Element).SetBigInt(h))

	return hElement, nil
}

func rho(phi B32le, high uint32) (*fr.Element, error) {
	inputA := bigIntLE(phi[:])
	inputB := new(big.Int).SetUint64(uint64(high))
	input := []*big.Int{inputA, inputB}

	cons, err := poseidon.GenPoseidonConstants[*CursedPoseidonGenRandomnessElement](3)
	if err != nil {
		return nil, err
	}

	// Compute the hash
	h, err := poseidon.Hash(input, cons, poseidon.OptimizedStatic)
	if err != nil {
		return nil, err
	}

	return new(fr.Element).SetBigInt(h), nil
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
	log.Printf("NewInvRange: h=%d, nodesCount=%d, offset=%d, num=%d", h, nodesCount, offset, num)

	bitsShr := calcBitsShr(h, nodesCount)
	highRange := calcHighRange(offset, num, bitsShr)

	log.Printf("bitsShr: %d", bitsShr)
	log.Printf("highRange: %v", highRange)

	rhos := make(map[uint64]B32le)
	for high := highRange.Start; high <= highRange.End; high++ {
		rhoVal, err := rho(phi, uint32(high))
		if err != nil {
			return nil, err
		}

		invRho := new(fr.Element).Inverse(rhoVal) // same as blst_fr_eucl_inverse??
		rhos[high] = ffElementBytesLE(invRho)

		log.Printf("rho[%d]: %x", high, rhos[high])
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
	nodeIndexBitLen := uint64(bits.TrailingZeros64(nodesCount))
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

func ffElementBytesLE(z *fr.Element) (res B32le) {
	fr.LittleEndian.PutElement(&res, *z)
	return
}

func bigIntLE(in BytesLE) *big.Int {
	// copy to b
	b := make([]byte, len(in))
	copy(b, in)

	// invert
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	// SetBytes is BE, so we needed to invert
	return new(big.Int).SetBytes(b)
}

/////
// Sanity lost beyond this point

type CursedPoseidonGenRandomnessElement struct {
	*fr.Element
}

func (c *CursedPoseidonGenRandomnessElement) SetUint64(u uint64) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.SetUint64(u)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) SetBigInt(b *big.Int) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.SetBigInt(b)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) SetBytes(bytes []byte) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.SetBytes(bytes)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) BigInt(b *big.Int) *big.Int {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	return c.Element.BigInt(b)
}

func (c *CursedPoseidonGenRandomnessElement) SetOne() *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.SetOne()
	return c
}

func (c *CursedPoseidonGenRandomnessElement) SetZero() *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.SetZero()
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Inverse(e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Inverse(e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Set(e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Set(e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Square(e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Square(e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Mul(e2 *CursedPoseidonGenRandomnessElement, e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Mul(e2.Element, e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Add(e2 *CursedPoseidonGenRandomnessElement, e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Add(e2.Element, e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Sub(e2 *CursedPoseidonGenRandomnessElement, e *CursedPoseidonGenRandomnessElement) *CursedPoseidonGenRandomnessElement {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	c.Element = c.Element.Sub(e2.Element, e.Element)
	return c
}

func (c *CursedPoseidonGenRandomnessElement) Cmp(x *CursedPoseidonGenRandomnessElement) int {
	if c.Element == nil {
		c.Element = new(fr.Element)
	}

	return c.Element.Cmp(x.Element)
}

func (c *CursedPoseidonGenRandomnessElement) SetString(s string) (*CursedPoseidonGenRandomnessElement, error) {
	if s == "3" {
		whatTheFuck := "0000000000010000000000000000000000000000000000000000000000000000"
		dstLE := must.One(hex.DecodeString(whatTheFuck))
		inverted := make([]byte, len(dstLE))
		for i := 0; i < len(dstLE); i++ {
			inverted[i] = dstLE[len(dstLE)-1-i]
		}

		c.SetBytes(inverted)
		return c, nil
	}

	el, err := c.Element.SetString(s)
	if err != nil {
		return nil, err
	}

	c.Element = el
	return c, nil
}

var _ poseidon.Element[*CursedPoseidonGenRandomnessElement] = &CursedPoseidonGenRandomnessElement{}
