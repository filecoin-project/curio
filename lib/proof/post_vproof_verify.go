package proof

import (
	"bytes"
	"fmt"
	poseidondst "github.com/filecoin-project/curio/lib/proof/poseidon"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/sync/errgroup"
	"math"
	"math/big"
	"sync"

	"golang.org/x/xerrors"

	// Example Poseidon library (a stand-in for your actual Poseidon):
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/proof"
	"github.com/triplewz/poseidon"
)

var log = logging.Logger("proof")

func VerifyWindowPoStVanilla(pvi proof.WindowPoStVerifyInfo) (bool, error) {
	var rand [32]byte
	if copy(rand[:], pvi.Randomness[:]) != 32 {
		return false, fmt.Errorf("randomness is not 32 bytes")
	}

	replicas := make(map[uint64]PublicReplicaInfo)
	for _, info := range pvi.ChallengedSectors {
		commr, err := commcid.CIDToReplicaCommitmentV1(info.SealedCID)
		if err != nil {
			return false, fmt.Errorf("failed to get replica commitment: %w", err)
		}

		log.Debugf("commr: %x", commr)

		replicas[uint64(info.SectorNumber)] = PublicReplicaInfo{
			CommR: PoseidonDomain(commr),
		}
	}

	if len(pvi.Proofs) != 1 {
		return false, fmt.Errorf("expected 1 proof, got %d", len(pvi.Proofs))
	}

	vp, err := DecodeFallbackPoStSectorProof(bytes.NewReader(pvi.Proofs[0].ProofBytes))
	if err != nil {
		return false, fmt.Errorf("failed to decode fallback PoSt sector proof: %w", err)
	}

	ssize, err := pvi.Proofs[0].PoStProof.SectorSize()
	if err != nil {
		return false, fmt.Errorf("failed to get sector size: %w", err)
	}

	config := GetPoStConfig(ssize)

	return verifyWindowPoStVanilla(config, rand, replicas, []FallbackPoStSectorProof{vp})
}

// VerifyWindowPoStVanilla checks the correctness of the merkle inclusion paths & CommR for
// each `FallbackPoStSectorProof`. This matches the fallback post logic from rust-fil-proofs,
// verifying the “vanilla” portion (no SNARK).
func verifyWindowPoStVanilla(
	cfg *PoStConfig,
	randomness [32]byte,
	replicas map[uint64]PublicReplicaInfo,
	sectorProofs []FallbackPoStSectorProof,
) (bool, error) {

	if cfg.PoStType != PoStTypeWindow {
		return false, xerrors.Errorf("expected Window PoSt, got type=%d", cfg.PoStType)
	}

	// Partition if the number of sectors is large
	partitions, err := partitionFallbackPoStSectorProofs(cfg, sectorProofs)
	if err != nil {
		return false, xerrors.Errorf("failed partitioning: %w", err)
	}

	for pIndex, partition := range partitions {
		var eg errgroup.Group

		for i, sec := range partition {
			i := i
			sec := sec

			eg.Go(func() error {
				pub, ok := replicas[sec.SectorID]
				if !ok {
					return fmt.Errorf("missing PublicReplicaInfo for sector %d", sec.SectorID)
				}
				// Check top-level comm_r
				if pub.CommR != sec.CommR {
					return fmt.Errorf("CommR mismatch for sector %d in partition %d", sec.SectorID, pIndex)
				}

				// Inspect the “VanillaProof” => array of SectorProof objects
				for _, sp := range sec.VanillaProof.Sectors {
					// fallback post logic typically does comm_r == PoseidonHash2(sp.comm_c, sp.comm_r_last)
					if !checkCommR(sp.CommC, sp.CommRLast, sec.CommR) {
						return fmt.Errorf("comm_r invalid: sector %d, partition %d", sec.SectorID, pIndex)
					}

					// Then verify the merkle inclusion proofs
					for _, mp := range sp.InclusionProofs {
						root, err := checkMerkleProof(mp)
						if err != nil {
							return xerrors.Errorf("merkle proof error for sector %d: %w", sec.SectorID, err)
						}
						if !bytes.Equal(root[:], sp.CommRLast[:]) {
							return fmt.Errorf("comm_r_last mismatch from merkle root in sector %d", sec.SectorID)
						}
					}
				}

				fmt.Println(i)
				return nil
			})
		}

		if err := eg.Wait(); err != nil {
			return false, err
		}
	}

	return true, nil
}

// partitionFallbackPoStSectorProofs splits sector proofs into chunks of length cfg.SectorCount.
func partitionFallbackPoStSectorProofs(cfg *PoStConfig, all []FallbackPoStSectorProof) ([][]FallbackPoStSectorProof, error) {
	partitionCount := int(math.Ceil(float64(len(all)) / float64(cfg.SectorCount)))
	if partitionCount < 1 {
		partitionCount = 1
	}

	out := make([][]FallbackPoStSectorProof, 0, partitionCount)
	start := 0
	for i := 0; i < partitionCount; i++ {
		end := start + cfg.SectorCount
		if end > len(all) {
			end = len(all)
		}
		chunk := all[start:end]
		start = end

		// If chunk is too small, pad with last
		if len(chunk) < cfg.SectorCount && len(chunk) > 0 {
			pad := make([]FallbackPoStSectorProof, 0, cfg.SectorCount-len(chunk))
			last := chunk[len(chunk)-1]
			for len(chunk)+len(pad) < cfg.SectorCount {
				pad = append(pad, last)
			}
			chunk = append(chunk, pad...)
		}
		out = append(out, chunk)
	}
	return out, nil
}

// checkCommR is the fallback post condition: comm_r = PoseidonHash2(comm_c, comm_r_last).
func checkCommR(commC, commRLast, claimed PoseidonDomain) bool {
	computed := poseidonHashMulti[poseidondst.Arity2]([]PoseidonDomain{commC, commRLast})
	return bytes.Equal(computed[:], claimed[:])
}

// checkMerkleProof dispatches to single/sub/top proof reconstruction.
func checkMerkleProof(mp MerkleProof[PoseidonDomain]) (PoseidonDomain, error) {
	switch {
	case mp.Data.Single != nil:
		log.Debugf("checking single proof")
		return doSingleProof(*mp.Data.Single)
	case mp.Data.Sub != nil:
		log.Debugf("checking sub proof")
		return doSubProof(*mp.Data.Sub)
	case mp.Data.Top != nil:
		log.Debugf("checking top proof")
		return doTopProof(*mp.Data.Top)
	default:
		return PoseidonDomain{}, fmt.Errorf("invalid merkle proof variant: no single/sub/top set")
	}
}

// SingleProof => reconstruct root from Leaf + Path => check equals sp.Root
func doSingleProof(sp SingleProof[PoseidonDomain]) (PoseidonDomain, error) {
	log.Debugf("single proof leaf: %x", sp.Leaf[:])
	final := reconstructPath(sp.Leaf, sp.Path)
	log.Debugf("single proof computed root: %x", final[:])
	log.Debugf("single proof expected root: %x", sp.Root[:])
	if !bytes.Equal(final[:], sp.Root[:]) {
		return PoseidonDomain{}, fmt.Errorf("single proof mismatch root")
	}
	return final, nil
}

// SubProof => base => sub => compare with Root
func doSubProof(sp SubProof[PoseidonDomain]) (PoseidonDomain, error) {
	log.Debugf("sub proof leaf: %x", sp.Leaf[:])
	baseOut := reconstructPath(sp.Leaf, sp.BaseProof)
	log.Debugf("sub proof base output: %x", baseOut[:])
	subOut := reconstructPath(baseOut, sp.SubProof)
	log.Debugf("sub proof computed root: %x", subOut[:])
	log.Debugf("sub proof expected root: %x", sp.Root[:])
	if !bytes.Equal(subOut[:], sp.Root[:]) {
		return PoseidonDomain{}, fmt.Errorf("sub proof mismatch root")
	}
	return subOut, nil
}

// TopProof => base => sub => top => compare with Root
func doTopProof(tp TopProof[PoseidonDomain]) (PoseidonDomain, error) {
	log.Debugf("top proof leaf: %x", tp.Leaf[:])
	base := reconstructPath(tp.Leaf, tp.BaseProof)
	log.Debugf("top proof base output: %x", base[:])
	sub := reconstructPath(base, tp.SubProof)
	log.Debugf("top proof sub output: %x", sub[:])
	top := reconstructPath(sub, tp.TopProof)
	log.Debugf("top proof computed root: %x", top[:])
	log.Debugf("top proof expected root: %x", tp.Root[:])
	if !bytes.Equal(top[:], tp.Root[:]) {
		return PoseidonDomain{}, fmt.Errorf("top proof mismatch root")
	}
	return top, nil
}

// reconstructPath merges each path element using poseidonHashMulti with the "Index" deciding
// where the current node is inserted among the siblings.
func reconstructPath(leaf PoseidonDomain, path InclusionPath[PoseidonDomain]) PoseidonDomain {
	cur := leaf
	for i, elem := range path.Path {
		arity := len(elem.Hashes) + 1
		idx := int(elem.Index)
		log.Debugf("path element %d: index=%d arity=%d", i, idx, arity)
		combined := make([]PoseidonDomain, arity)
		j := 0
		for x := 0; x < arity; x++ {
			if x == idx {
				combined[x] = cur
				log.Debugf("path element %d: position %d = current %x", i, x, cur[:])
			} else {
				combined[x] = elem.Hashes[j]
				log.Debugf("path element %d: position %d = sibling %x", i, x, elem.Hashes[j][:])
				j++
			}
		}
		cur = poseidonHashMulti[poseidondst.Arity8](combined)
		log.Debugf("path element %d: hash result %x", i, cur[:])
	}
	return cur
}

var consts = map[int]any{}
var lk = sync.Mutex{}

// poseidonHashMulti merges an arbitrary slice of domain elements in one Poseidon invocation.
func poseidonHashMulti[A poseidondst.Arity](vals []PoseidonDomain) PoseidonDomain {
	arity := (*new(A)).Arity()
	if len(vals) != arity {
		panic("poseidonhashMulti called with invalid amount of values")
	}

	type E = *poseidondst.DSTElement[poseidondst.MerkleTreeDST[A]]

	var cons *poseidon.PoseidonConst[E]
	var err error

	lk.Lock()
	if consts[arity] != nil {
		cons = consts[arity].(*poseidon.PoseidonConst[E])
	} else {
		cons, err = poseidon.GenPoseidonConstants[E](len(vals) + 1)
		if err != nil {
			panic(fmt.Sprintf("poseidonHashMulti constants: %v", err))
		}
		consts[arity] = cons
	}
	lk.Unlock()

	bigs := make([]*big.Int, len(vals))
	for i, v := range vals {
		bigs[i] = domainToBigInt(v)
		//log.Debugf("poseidon input %d: %x", i, v[:])
	}

	h, err := poseidon.Hash(bigs, cons, poseidon.OptimizedStatic)
	if err != nil {
		panic(fmt.Sprintf("poseidonHashMulti error: %v", err))
	}
	result := bigIntToDomain(h)
	//log.Debugf("poseidon output: %x", result[:])
	return result
}

// domainToBigInt interprets a PoseidonDomain as a little-endian integer.
func domainToBigInt(d PoseidonDomain) *big.Int {
	// Reverse to big-endian for .SetBytes
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[31-i] = d[i]
	}
	return new(big.Int).SetBytes(be)
}

// bigIntToDomain converts a field-limited big.Int back into [32]byte LE.
func bigIntToDomain(x *big.Int) PoseidonDomain {
	// We do fr.Element => [32]byte LE. So first do: e.SetBigInt(x), then e.BytesLE
	var el fr.Element
	el.SetBigInt(x)
	return ffElementBytesLE(&el)
}

// ffElementBytesLE writes fr.Element in LE form into a [32]byte.
func ffElementBytesLE(z *fr.Element) PoseidonDomain {
	// gnark-crypto's fr.LittleEndian provides "PutElement".
	// We'll do it manually for illustration:
	// 1) get big-endian bytes from z, 2) reverse to [32]byte LE.
	be := z.Bytes() // 32 bytes, big-endian
	var out PoseidonDomain
	for i := 0; i < 32; i++ {
		out[i] = be[31-i]
	}
	return out
}
