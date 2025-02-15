package poseidondst

import (
	"encoding/hex"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	"github.com/snadrus/must"
	"github.com/triplewz/poseidon"
)

// Neptune-style domain separation.
//
// The Neptune Rust code uses (in pseudo-Rust):
//
// ┌────────────────────────────┬───────────────────────────────────────────────┐
// │ HashType                  │ domain_tag()                                 │
// ├────────────────────────────┼───────────────────────────────────────────────┤
// │ MerkleTree                │ 2^arity - 1                                  │
// │ MerkleTreeSparse(bitmask) │ bitmask (directly as a field element)        │
// │ VariableLength            │ 2^64                                         │
// │ ConstantLength(len)       │ len * 2^64                                   │
// │ Encryption                │ 2^32                                         │
// │ Custom(id)                │ id * 2^40                                    │
// │ Sponge                    │ 0                                            │
// └────────────────────────────┴───────────────────────────────────────────────┘
//
// The code below demonstrates how to compute each of these in Go, returning a fr.Element.

type DST interface {
	DST() *fr.Element
}

type MerkleTreeDST[A Arity] struct{}

func (m MerkleTreeDST[A]) DST() *fr.Element {
	arity := (*new(A)).Arity()
	twoToArityMinus1 := twoToArityMinus1(uint64(arity))
	return &twoToArityMinus1
}

type MerkleTree8 = DSTElement[MerkleTreeDST[Arity8]]
type MerkleTree2 = DSTElement[MerkleTreeDST[Arity2]]

/*

type HashType int

const (
    HashTypeMerkleTree HashType = iota
    HashTypeMerkleTreeSparse
    HashTypeVariableLength
    HashTypeConstantLength
    HashTypeEncryption
    HashTypeCustom
    HashTypeSponge
)

// DomainTagParams includes parameters needed to compute the domain tag,
// such as "arity" (for MerkleTree), "bitmask" (for MerkleTreeSparse),
// "length" (for ConstantLength), and "id" (for Custom).
type DomainTagParams struct {
    Arity   uint64 // For MerkleTree
    Bitmask uint64 // For MerkleTreeSparse
    Length  uint64 // For ConstantLength
    ID      uint64 // For Custom
}

// DomainTag computes the domain tag as a BLS12-381 field element
// according to Neptune’s scheme. The `params` struct supplies whichever
// parameter is relevant for the chosen HashType.
func DomainTag(ht HashType, params DomainTagParams) fr.Element {
    switch ht {
    case HashTypeMerkleTree:
        // 2^arity - 1
        return twoToArityMinus1(params.Arity)

    case HashTypeMerkleTreeSparse:
        // bitmask
        var out fr.Element
        out.SetUint64(params.Bitmask)
        return out

    case HashTypeVariableLength:
        // 2^64
        return pow2(64)

    case HashTypeConstantLength:
        // length * 2^64
        return xPow2(params.Length, 64)

    case HashTypeEncryption:
        // 2^32
        return pow2(32)

    case HashTypeCustom:
        // id * 2^40
        // id must be in [1..=256] in Neptune's reference code to avoid collisions.
        if params.ID == 0 || params.ID > 256 {
            panic("invalid custom domain tag ID")
        }
        return xPow2(params.ID, 40)

    case HashTypeSponge:
        // 0
        var out fr.Element
        out.SetZero()
        return out

    default:
        panic("unrecognized HashType")
    }
}

// twoToArityMinus1 => 2^arity - 1
func twoToArityMinus1(arity uint64) fr.Element {
    base := fr.NewElement(2) // Start with 2
    exponent := big.NewInt(int64(arity))
    base.Exp(base, exponent) // base = 2^arity

    // Now subtract 1
    var one fr.Element
    one.SetOne()
    base.Sub(&base, &one)

    return base
}
*/

// Custon Snap

type SnapDST struct{}

func (s SnapDST) DST() *fr.Element {
	// pub const HASH_TYPE_GEN_RANDOMNESS: HashType<Fr, U2> = HashType::Custom(CType::Arbitrary(1));
	genRandomnessDST := "0000000000010000000000000000000000000000000000000000000000000000"
	dstLE := must.One(hex.DecodeString(genRandomnessDST))
	inverted := make([]byte, len(dstLE))
	for i := 0; i < len(dstLE); i++ {
		inverted[i] = dstLE[len(dstLE)-1-i]
	}

	var c fr.Element
	c.SetBytes(inverted)

	return &c
}

type SnapElement = DSTElement[SnapDST]

// DSTElement is a custom element which overrides SetString used by the poseidon hash function to set
// the default hardcoded DST. We hijack the SetString function to set the DST to the hardcoded value needed for Snap.
type DSTElement[D DST] struct {
	fr.Element
}

func (c *DSTElement[D]) SetUint64(u uint64) *DSTElement[D] {
	c.Element = *(&c.Element).SetUint64(u)
	return c
}

func (c *DSTElement[D]) SetBigInt(b *big.Int) *DSTElement[D] {
	c.Element = *c.Element.SetBigInt(b)
	return c
}

func (c *DSTElement[D]) SetBytes(bytes []byte) *DSTElement[D] {
	c.Element = *c.Element.SetBytes(bytes)
	return c
}

func (c *DSTElement[D]) BigInt(b *big.Int) *big.Int {
	return c.Element.BigInt(b)
}

func (c *DSTElement[D]) SetOne() *DSTElement[D] {
	c.Element = *c.Element.SetOne()
	return c
}

func (c *DSTElement[D]) SetZero() *DSTElement[D] {
	c.Element = *c.Element.SetZero()
	return c
}

func (c *DSTElement[D]) Inverse(e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Inverse(&e.Element)
	return c
}

func (c *DSTElement[D]) Set(e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Set(&e.Element)
	return c
}

func (c *DSTElement[D]) Square(e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Square(&e.Element)
	return c
}

func (c *DSTElement[D]) Mul(e2 *DSTElement[D], e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Mul(&e2.Element, &e.Element)
	return c
}

func (c *DSTElement[D]) Add(e2 *DSTElement[D], e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Add(&e2.Element, &e.Element)
	return c
}

func (c *DSTElement[D]) Sub(e2 *DSTElement[D], e *DSTElement[D]) *DSTElement[D] {
	c.Element = *c.Element.Sub(&e2.Element, &e.Element)
	return c
}

func (c *DSTElement[D]) Cmp(x *DSTElement[D]) int {
	return c.Element.Cmp(&x.Element)
}

func (c *DSTElement[D]) New() *DSTElement[D] {
	return new(DSTElement[D])
}

func (c *DSTElement[D]) SetString(s string) (*DSTElement[D], error) {
	if s == "3" {
		c.Element = *(*new(D)).DST()
		return c, nil
	}

	el, err := c.Element.SetString(s)
	if err != nil {
		return nil, err
	}

	c.Element = *el
	return c, nil
}

// twoToArityMinus1 => 2^arity - 1
func twoToArityMinus1(arity uint64) fr.Element {
	base := fr.NewElement(2) // Start with 2
	exponent := big.NewInt(int64(arity))
	base.Exp(base, exponent) // base = 2^arity

	// Now subtract 1
	var one fr.Element
	one.SetOne()
	base.Sub(&base, &one)

	return base
}

// pow2(exp) => 2^exp
func pow2(exp uint64) fr.Element {
	base := fr.NewElement(2)
	exponent := big.NewInt(int64(exp))
	base.Exp(base, exponent) // base = 2^exp
	return base
}

// xPow2(x, exp) => x * 2^exp
func xPow2(x uint64, exp uint64) fr.Element {
	// 2^exp
	twoExp := pow2(exp)

	// x * 2^exp
	var out fr.Element
	// Convert x to fr.Element
	var feX fr.Element
	feX.SetUint64(x)
	out.Mul(&feX, &twoExp)
	return out
}

var _ poseidon.Element[*DSTElement[SnapDST]] = &DSTElement[SnapDST]{}
