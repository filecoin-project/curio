package proof

import (
	"fmt"
	poseidondst "github.com/filecoin-project/curio/lib/proof/poseidon"
)

// CommRLastFromTreeRLastRoots computes comm_r_last from the roots of the tree-r-last
// partition files (each root is the last 32 bytes of sc-02-data-tree-r-last[-N].dat).
//
// Important: the Poseidon hashing here must match the rust-fil-proofs "compound merkle tree"
// reduction. In particular, when there are 16 partitions, fil-proofs reduces 16 -> 2 using
// an 8-arity hash twice, and then hashes those 2 with arity-2.
func CommRLastFromTreeRLastRoots(roots []PoseidonDomain) (PoseidonDomain, error) {
	switch len(roots) {
	case 0:
		return PoseidonDomain{}, fmt.Errorf("no tree-r-last roots provided")
	case 1:
		return roots[0], nil
	case 2:
		return poseidonHashMulti[poseidondst.Arity2](roots), nil
	case 4:
		return poseidonHashMulti[poseidondst.Arity4](roots), nil
	case 8:
		return poseidonHashMulti[poseidondst.Arity8](roots), nil
	case 16:
		h0 := poseidonHashMulti[poseidondst.Arity8](roots[:8])
		h1 := poseidonHashMulti[poseidondst.Arity8](roots[8:])
		return poseidonHashMulti[poseidondst.Arity2]([]PoseidonDomain{h0, h1}), nil
	default:
		return PoseidonDomain{}, fmt.Errorf("unsupported tree-r-last partition count %d", len(roots))
	}
}


