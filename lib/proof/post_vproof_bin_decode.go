package proof

import (
	"encoding/binary"
	"fmt"
	"io"
)

// DecodeFallbackPoStSectorProof decodes a single FallbackPoStSectorProof from the reader.
//
// Rust struct reference for context:
// #[derive(Clone, Debug, Serialize, Deserialize)]
//
//	pub struct FallbackPoStSectorProof<Tree: MerkleTreeTrait> {
//	    pub sector_id: SectorId,
//	    pub comm_r: <Tree::Hasher as Hasher>::Domain,
//	    pub vanilla_proof: Proof<<Tree as MerkleTreeTrait>::Proof>,
//	}
func DecodeFallbackPoStSectorProof(r io.Reader) (FallbackPoStSectorProof, error) {
	var out FallbackPoStSectorProof
	var err error

	// sector_id (u64)
	out.SectorID, err = ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode sector_id: %w", err)
	}

	// comm_r (PoseidonDomain = [32]byte)
	out.CommR, err = DecodeHasherDomain[PoseidonDomain](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode comm_r: %w", err)
	}

	// vanilla_proof
	out.VanillaProof, err = DecodeVanillaProof(r)
	if err != nil {
		return out, fmt.Errorf("failed to decode vanilla_proof: %w", err)
	}

	return out, nil
}

// DecodeVanillaProof decodes a VanillaProof from the reader.
//
// Rust struct reference for context:
//
//	pub struct Proof<P: MerkleProofTrait> {
//	    pub sectors: Vec<SectorProof<P>>
//	}
func DecodeVanillaProof(r io.Reader) (VanillaProof, error) {
	var out VanillaProof
	var err error

	// number of sectors
	var sectorsLength uint64
	if err = binary.Read(r, binary.LittleEndian, &sectorsLength); err != nil {
		return out, fmt.Errorf("failed to read sectors length: %w", err)
	}

	out.Sectors = make([]SectorProof, sectorsLength)
	for i := uint64(0); i < sectorsLength; i++ {
		out.Sectors[i], err = DecodeSectorProof(r)
		if err != nil {
			return out, fmt.Errorf("failed to decode SectorProof: %w", err)
		}
	}

	return out, nil
}

// DecodeSectorProof decodes a single SectorProof from the reader.
//
// Rust struct reference for context:
// #[derive(Debug, Clone, Serialize, Deserialize)]
//
//	pub struct SectorProof<Proof: MerkleProofTrait> {
//	    pub inclusion_proofs: Vec<Proof>,
//	    pub comm_c: <Proof::Hasher as Hasher>::Domain,
//	    pub comm_r_last: <Proof::Hasher as Hasher>::Domain,
//	}
func DecodeSectorProof(r io.Reader) (SectorProof, error) {
	var out SectorProof
	var err error

	// number of inclusion proofs
	var ipLength uint64
	if err = binary.Read(r, binary.LittleEndian, &ipLength); err != nil {
		return out, fmt.Errorf("failed to read inclusion_proofs length: %w", err)
	}

	out.InclusionProofs = make([]MerkleProof[PoseidonDomain], ipLength)
	for i := uint64(0); i < ipLength; i++ {
		out.InclusionProofs[i], err = DecodeMerkleProof[PoseidonDomain](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode MerkleProof: %w", err)
		}
	}

	// comm_c (PoseidonDomain = [32]byte)
	out.CommC, err = DecodeHasherDomain[PoseidonDomain](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode comm_c: %w", err)
	}

	// comm_r_last (PoseidonDomain = [32]byte)
	out.CommRLast, err = DecodeHasherDomain[PoseidonDomain](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode comm_r_last: %w", err)
	}

	return out, nil
}
