package proof

import "github.com/minio/sha256-simd"

// VerifyProof walks a merkle inclusion proof from leaf to root using SHA254,
// returning true if the computed root matches the expected root.
//
// At each level, if the position is even the node is on the left (hash(node||sibling)),
// if odd the node is on the right (hash(sibling||node)). SHA254 masking (clear top
// 2 bits of byte 31) is applied after each hash for BLS12-381 field compatibility.
//
// This is a Go translation of the on-chain MerkleVerify.processInclusionProofMemory:
// https://github.com/FilOzone/pdp/blob/8ea3a99c11cc9194929408f603846e9965965ee5/src/Proofs.sol#L43-L60
func VerifyProof(leaf [32]byte, siblings [][32]byte, root [32]byte, position uint64) bool {
	computed := leaf
	for _, sibling := range siblings {
		if position%2 == 0 {
			computed = sha256.Sum256(append(computed[:], sibling[:]...))
		} else {
			computed = sha256.Sum256(append(sibling[:], computed[:]...))
		}
		computed[31] &= 0x3F
		position /= 2
	}
	return computed == root
}
