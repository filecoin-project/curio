package cunative

// #cgo CFLAGS: -I${SRCDIR}/../../../extern/supra_seal/deps/blst/bindings
// #cgo LDFLAGS: -L${SRCDIR}/../../../extern/supra_seal/deps/blst -lblst
// #include <stdint.h>
// #include <stdlib.h>
// #include "blst.h"
//
// void snap_decode_loop(const uint8_t *replica, const uint8_t *key, const uint8_t *rho_invs, uint8_t *out, size_t node_count, size_t node_size) {
//     blst_fr replica_fr, key_fr, rho_inv_fr, out_fr;
//
//     for (size_t i = 0; i < node_count; i++) {
//         // Read replica data
//         blst_fr_from_lendian(&replica_fr, replica + i * node_size);
//
//         // Read key data
//         blst_fr_from_lendian(&key_fr, key + i * node_size);
//
//         // Read rho inverse
//         blst_fr_from_lendian(&rho_inv_fr, rho_invs + i * 32);  // Assuming rho_invs are 32 bytes each
//
//         // Perform the decoding operation
//         blst_fr_sub(&out_fr, &replica_fr, &key_fr);
//         blst_fr_mul(&out_fr, &out_fr, &rho_inv_fr);
//
//         // Write the result
//         blst_fr_to_lendian(out + i * node_size, &out_fr);
//     }
// }
import "C"

import (
	"bufio"
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
)

/*

   let comm_r_old = <TreeRHasher as Hasher>::Function::hash2(&comm_c, &comm_sector_key);
        let phi = phi(&comm_d_new, &comm_r_old);
        let chunk_size: usize = std::cmp::min(base_tree_nodes_count, CHUNK_SIZE_MIN);

        let rho_invs = Rhos::new_inv(&phi, h, nodes_count);



Reference:
func CommR(commC, commRLast [32]byte) ([32]byte, error) {
	// reverse commC and commRLast so that endianness is correct
	for i, j := 0, len(commC)-1; i < j; i, j = i+1, j-1 {
		commC[i], commC[j] = commC[j], commC[i]
		commRLast[i], commRLast[j] = commRLast[j], commRLast[i]
	}

	input_a := new(big.Int)
	input_a.SetBytes(commC[:])
	input_b := new(big.Int)
	input_b.SetBytes(commRLast[:])
	input := []*big.Int{input_a, input_b}

	cons, err := poseidon.GenPoseidonConstants(3)
	if err != nil {
		return [32]byte{}, err
	}

	h1, err := poseidon.Hash(input, cons, poseidon.OptimizedStatic)
	if err != nil {
		return [32]byte{}, err
	}

	h1element := new(ff.Element).SetBigInt(h1).Bytes()

	// reverse the bytes so that endianness is correct
	for i, j := 0, len(h1element)-1; i < j; i, j = i+1, j-1 {
		h1element[i], h1element[j] = h1element[j], h1element[i]
	}

	return h1element, nil
}

impl HashFunction<PoseidonDomain> for PoseidonFunction {
    fn hash(data: &[u8]) -> PoseidonDomain {
        shared_hash(data)
    }

    fn hash2(a: &PoseidonDomain, b: &PoseidonDomain) -> PoseidonDomain {
        let mut p =
            Poseidon::new_with_preimage(&[(*a).into(), (*b).into()][..], &*POSEIDON_CONSTANTS_2);
        let fr: Fr = p.hash();
        fr.into()
    }
...
        let comm_r_old = <TreeRHasher as Hasher>::Function::hash2(&comm_c, &comm_sector_key);

...


    /// Creates [`Poseidon`] instance using provided preimage and [`PoseidonConstants`] as input.
    /// Doesn't support [`PoseidonConstants`] with [`HashType::VariableLength`]. It is assumed that
    /// size of input preimage set can't be greater than [`Arity`].
    ///
    /// # Example
    ///
    /// ```
    /// use neptune::poseidon::PoseidonConstants;
    /// use neptune::poseidon::Poseidon;
    /// use pasta_curves::Fp;
    /// use ff::Field;
    /// use generic_array::typenum::U2;
    ///
    /// let preimage_set_length = 1;
    /// let constants: PoseidonConstants<Fp, U2> = PoseidonConstants::new_constant_length(preimage_set_length);
    ///
    /// let preimage = vec![Fp::from(u64::MAX); preimage_set_length];
    ///
    /// let mut poseidon = Poseidon::<Fp, U2>::new_with_preimage(&preimage, &constants);
    ///
    /// assert_eq!(constants.width(), 3);
    /// assert_eq!(poseidon.elements.len(), constants.width());
    /// assert_eq!(poseidon.elements[1], Fp::from(u64::MAX));
    /// assert_eq!(poseidon.elements[2], Fp::ZERO);
    /// ```
    pub fn new_with_preimage(preimage: &[F], constants: &'a PoseidonConstants<F, A>) -> Self {
        let elements = match constants.hash_type {
            HashType::ConstantLength(constant_len) => {
                assert_eq!(constant_len, preimage.len(), "Invalid preimage size");

                GenericArray::generate(|i| {
                    if i == 0 {
                        constants.domain_tag
                    } else if i > preimage.len() {
                        F::ZERO
                    } else {
                        preimage[i - 1]
                    }
                })
            }
            HashType::MerkleTreeSparse(_) => {
                panic!("Merkle Tree (with some empty leaves) hashes are not yet supported.")
            }
            HashType::VariableLength => panic!("variable-length hashes are not yet supported."),
            _ => {
                assert_eq!(preimage.len(), A::to_usize(), "Invalid preimage size");

                GenericArray::generate(|i| {
                    if i == 0 {
                        constants.domain_tag
                    } else {
                        preimage[i - 1]
                    }
                })
            }
        };
        let width = preimage.len() + 1;

        Poseidon {
            constants_offset: 0,
            current_round: 0,
            elements,
            pos: width,
            constants,
            _f: PhantomData::<F>,
        }
    }

// Use a custom domain separation tag when generating randomness phi, rho, and challenges bits.
pub const HASH_TYPE_GEN_RANDOMNESS: HashType<Fr, U2> = HashType::Custom(CType::Arbitrary(1));

lazy_static! {
    pub static ref POSEIDON_CONSTANTS_GEN_RANDOMNESS: PoseidonConstants::<Fr, U2> =
        PoseidonConstants::new_with_strength_and_type(Strength::Standard, HASH_TYPE_GEN_RANDOMNESS);
}


// `phi = H(comm_d_new, comm_r_old)` where Poseidon uses the custom "gen randomness" domain
// separation tag.
#[inline]
pub fn phi<TreeDDomain: Domain>(comm_d_new: &TreeDDomain, comm_r_old: &TreeRDomain) -> TreeRDomain {
    let comm_d_new: Fr = (*comm_d_new).into();
    let comm_r_old: Fr = (*comm_r_old).into();
    Poseidon::new_with_preimage(
        &[comm_d_new, comm_r_old],
        &POSEIDON_CONSTANTS_GEN_RANDOMNESS,
    )
    .hash()
    .into()
}

*/

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

	// Setup buffered readers and writers
	keyReader := bufio.NewReader(key)
	replicaReader := bufio.NewReader(replica)
	outWriter := bufio.NewWriter(out)

	// Process data in chunks
	const dataBlockSize = 1 << 10
	buffer := make([]byte, dataBlockSize)
	for chunkIndex := uint64(0); ; chunkIndex += dataBlockSize {
		n, err := io.ReadFull(replicaReader, buffer)
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return xerrors.Errorf("failed to read replica data: %w", err)
		}

		for i := uint64(0); i < uint64(n); i += proof.NODE_SIZE {
			inputIndex := chunkIndex + i
			nodeIndex := inputIndex / proof.NODE_SIZE

			// Read sector key data
			sectorKeyData := make([]byte, proof.NODE_SIZE)
			_, err := io.ReadFull(keyReader, sectorKeyData)
			if err != nil {
				return xerrors.Errorf("failed to read sector key data: %w", err)
			}

			// Convert bytes to field elements
			sectorKeyFr := bytesToFr(sectorKeyData)
			replicaDataFr := bytesToFr(buffer[i : i+proof.NODE_SIZE])

			// Get rho inverse
			rhoInv := rhoInvs.Get(nodeIndex)

			// Perform the decoding operation
			outDataFr := frSub(replicaDataFr, sectorKeyFr)
			outDataFr = frMul(outDataFr, rhoInv)

			// Convert back to bytes and write
			outData := frToBytes(outDataFr)
			_, err = outWriter.Write(outData)
			if err != nil {
				return xerrors.Errorf("failed to write output data: %w", err)
			}
		}
	}

	err = outWriter.Flush()
	if err != nil {
		return xerrors.Errorf("failed to flush output: %w", err)
	}

	return nil
}

// Phi implements the phi function as described in the Rust code.
// It computes phi = H(comm_d_new, comm_r_old) using Poseidon hash with a custom domain separation tag.
func Phi(commDNew, commROld []byte) ([32]byte, error) {
	// Reverse commDNew and commROld for correct endianness
	for i, j := 0, len(commDNew)-1; i < j; i, j = i+1, j-1 {
		commDNew[i], commDNew[j] = commDNew[j], commDNew[i]
		commROld[i], commROld[j] = commROld[j], commROld[i]
	}

	inputA := new(big.Int).SetBytes(commDNew[:])
	inputB := new(big.Int).SetBytes(commROld[:])
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

	hElement := new(ff.Element).SetBigInt(h).Bytes()

	// Reverse the bytes for correct endianness
	for i, j := 0, len(hElement)-1; i < j; i, j = i+1, j-1 {
		hElement[i], hElement[j] = hElement[j], hElement[i]
	}

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
	rhos    map[uint64]ff.Element
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

	rhos := make(map[uint64]ff.Element)
	for high := highRange.Start; high <= highRange.End; high++ {
		rhoVal, err := rho(phi, uint32(high))
		if err != nil {
			return nil, err
		}

		invRho := new(ff.Element).Inverse(rhoVal) // same as blst_fr_eucl_inverse??
		rhos[high] = *invRho
	}

	return &Rhos{
		rhos:    rhos,
		bitsShr: bitsShr,
	}, nil
}

// Get retrieves the rho for a specific node offset
func (r *Rhos) Get(offset uint64) ff.Element {
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
