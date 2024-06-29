package proof

import (
	"encoding/binary"
	"fmt"
	"io"
)

func ReadLE[T any](r io.Reader) (T, error) {
	var out T
	err := binary.Read(r, binary.LittleEndian, &out)
	return out, err
}

func DecodeCommit1OutRaw(r io.Reader) (Commit1OutRaw, error) {
	var out Commit1OutRaw
	var err error

	out.RegisteredProof = "StackedDrg32GiBV1_1"

	// VanillaProofs
	out.VanillaProofs = make(map[StringRegisteredProofType][][]VanillaStackedProof)
	var vpOuterLength uint64
	if err = binary.Read(r, binary.LittleEndian, &vpOuterLength); err != nil {
		return out, fmt.Errorf("failed to read VanillaProofs outer length: %w", err)
	}

	for i := uint64(0); i < vpOuterLength; i++ {
		var vpInnerLength uint64
		if err = binary.Read(r, binary.LittleEndian, &vpInnerLength); err != nil {
			return out, fmt.Errorf("failed to read VanillaProofs inner length: %w", err)
		}

		proofs := make([]VanillaStackedProof, vpInnerLength)
		for j := uint64(0); j < vpInnerLength; j++ {
			proofs[j], err = DecodeVanillaStackedProof(r)
			if err != nil {
				return out, fmt.Errorf("failed to decode VanillaStackedProof: %w", err)
			}
		}

		key := StringRegisteredProofType("StackedDrg32GiBV1")
		out.VanillaProofs[key] = append(out.VanillaProofs[key], proofs)
	}

	// CommR
	if out.CommR, err = DecodeCommitment(r); err != nil {
		return out, fmt.Errorf("failed to decode CommR: %w", err)
	}

	// CommD
	if out.CommD, err = DecodeCommitment(r); err != nil {
		return out, fmt.Errorf("failed to decode CommD: %w", err)
	}

	// ReplicaID
	if out.ReplicaID, err = DecodeCommitment(r); err != nil {
		return out, fmt.Errorf("failed to decode ReplicaID: %w", err)
	}

	// Seed
	if out.Seed, err = DecodeTicket(r); err != nil {
		return out, fmt.Errorf("failed to decode Seed: %w", err)
	}

	// Ticket
	if out.Ticket, err = DecodeTicket(r); err != nil {
		return out, fmt.Errorf("failed to decode Ticket: %w", err)
	}

	// Read last byte, require EOF
	if _, err := r.Read(make([]byte, 1)); err != io.EOF {
		return out, fmt.Errorf("expected EOF")
	}

	return out, nil
}

func DecodeVanillaStackedProof(r io.Reader) (VanillaStackedProof, error) {
	var out VanillaStackedProof
	var err error

	if out.CommDProofs, err = DecodeMerkleProof[Sha256Domain](r); err != nil {
		return out, fmt.Errorf("failed to decode CommDProofs: %w", err)
	}
	if out.CommRLastProof, err = DecodeMerkleProof[PoseidonDomain](r); err != nil {
		return out, fmt.Errorf("failed to decode CommRLastProof: %w", err)
	}
	if out.ReplicaColumnProofs, err = DecodeReplicaColumnProof[PoseidonDomain](r); err != nil {
		return out, fmt.Errorf("failed to decode ReplicaColumnProofs: %w", err)
	}

	var numLabelingProofs uint64
	if err = binary.Read(r, binary.LittleEndian, &numLabelingProofs); err != nil {
		return out, fmt.Errorf("failed to read number of LabelingProofs: %w", err)
	}
	out.LabelingProofs = make([]LabelingProof[PoseidonDomain], numLabelingProofs)
	for i := uint64(0); i < numLabelingProofs; i++ {
		out.LabelingProofs[i], err = DecodeLabelingProof[PoseidonDomain](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode LabelingProof: %w", err)
		}
	}

	out.EncodingProof, err = DecodeEncodingProof[PoseidonDomain](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode EncodingProof: %w", err)
	}

	return out, nil
}

func DecodeMerkleProof[H HasherDomain](r io.Reader) (MerkleProof[H], error) {
	var out MerkleProof[H]
	var err error

	out.Data, err = DecodeProofData[H](r)
	if err != nil {
		return out, fmt.Errorf("failed to decode ProofData: %w", err)
	}

	return out, nil
}

func DecodeProofData[H HasherDomain](r io.Reader) (ProofData[H], error) {
	var out ProofData[H]
	var err error

	proofType, err := ReadLE[uint32](r)
	if err != nil {
		return out, fmt.Errorf("failed to read proof type: %w", err)
	}

	switch proofType {
	case 0:
		out.Single = new(SingleProof[H])
		*out.Single, err = DecodeSingleProof[H](r)
	case 1:
		out.Sub = new(SubProof[H])
		*out.Sub, err = DecodeSubProof[H](r)
	case 2:
		out.Top = new(TopProof[H])
		*out.Top, err = DecodeTopProof[H](r)
	default:
		return out, fmt.Errorf("unknown proof type: %d", proofType)
	}

	if err != nil {
		return out, fmt.Errorf("failed to decode proof: %w", err)
	}

	return out, nil
}

func DecodeSingleProof[H HasherDomain](r io.Reader) (SingleProof[H], error) {
	var out SingleProof[H]
	var err error

	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Root: %w", err)
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Leaf: %w", err)
	}
	if out.Path, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Path: %w", err)
	}

	return out, nil
}

func DecodeSubProof[H HasherDomain](r io.Reader) (SubProof[H], error) {
	var out SubProof[H]
	var err error

	if out.BaseProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode BaseProof: %w", err)
	}
	if out.SubProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode SubProof: %w", err)
	}
	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Root: %w", err)
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Leaf: %w", err)
	}

	return out, nil
}

func DecodeTopProof[H HasherDomain](r io.Reader) (TopProof[H], error) {
	var out TopProof[H]
	var err error

	if out.BaseProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode BaseProof: %w", err)
	}
	if out.SubProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode SubProof: %w", err)
	}
	if out.TopProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, fmt.Errorf("failed to decode TopProof: %w", err)
	}
	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Root: %w", err)
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Leaf: %w", err)
	}

	return out, nil
}

func DecodeInclusionPath[H HasherDomain](r io.Reader) (InclusionPath[H], error) {
	var out InclusionPath[H]
	var err error

	numElements, err := ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to read number of elements: %w", err)
	}

	out.Path = make([]PathElement[H], numElements)
	for i := uint64(0); i < numElements; i++ {
		out.Path[i], err = DecodePathElement[H](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode PathElement: %w", err)
		}
	}

	return out, nil
}

func DecodePathElement[H HasherDomain](r io.Reader) (PathElement[H], error) {
	var out PathElement[H]
	var err error

	numHashes, err := ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to read number of hashes: %w", err)
	}

	out.Hashes = make([]H, numHashes)
	for i := uint64(0); i < numHashes; i++ {
		out.Hashes[i], err = DecodeHasherDomain[H](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode HasherDomain: %w", err)
		}
	}

	out.Index, err = ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to read index: %w", err)
	}

	return out, nil
}

func DecodeReplicaColumnProof[H HasherDomain](r io.Reader) (ReplicaColumnProof[H], error) {
	var out ReplicaColumnProof[H]
	var err error

	if out.C_X, err = DecodeColumnProof[H](r); err != nil {
		return out, fmt.Errorf("failed to decode C_X: %w", err)
	}

	numDrgParents, err := ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to read number of DRG parents: %w", err)
	}
	out.DrgParents = make([]ColumnProof[H], numDrgParents)
	for i := uint64(0); i < numDrgParents; i++ {
		out.DrgParents[i], err = DecodeColumnProof[H](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode DRG parent: %w", err)
		}
	}

	numExpParents, err := ReadLE[uint64](r)
	if err != nil {
		return out, fmt.Errorf("failed to read number of EXP parents: %w", err)
	}
	out.ExpParents = make([]ColumnProof[H], numExpParents)
	for i := uint64(0); i < numExpParents; i++ {
		out.ExpParents[i], err = DecodeColumnProof[H](r)
		if err != nil {
			return out, fmt.Errorf("failed to decode EXP parent: %w", err)
		}
	}

	return out, nil
}

func DecodeColumnProof[H HasherDomain](r io.Reader) (ColumnProof[H], error) {
	var out ColumnProof[H]
	var err error

	if out.Column, err = DecodeColumn[H](r); err != nil {
		return out, fmt.Errorf("failed to decode Column: %w", err)
	}
	if out.InclusionProof, err = DecodeMerkleProof[H](r); err != nil {
		return out, fmt.Errorf("failed to decode InclusionProof: %w", err)
	}

	return out, nil
}

func DecodeColumn[H HasherDomain](r io.Reader) (Column[H], error) {
	var out Column[H]
	var err error

	if err = binary.Read(r, binary.LittleEndian, &out.Index); err != nil {
		return out, fmt.Errorf("failed to decode Index: %w", err)
	}

	var rowsLength uint64
	if err = binary.Read(r, binary.LittleEndian, &rowsLength); err != nil {
		return out, fmt.Errorf("failed to read Rows length: %w", err)
	}
	out.Rows = make([]H, rowsLength)
	for i := uint64(0); i < rowsLength; i++ {
		if out.Rows[i], err = DecodeHasherDomain[H](r); err != nil {
			return out, fmt.Errorf("failed to decode Row: %w", err)
		}
	}

	// Note: We're ignoring the _h field as it's not used in Go

	return out, nil
}

func DecodeLabelingProof[H HasherDomain](r io.Reader) (LabelingProof[H], error) {
	var out LabelingProof[H]
	var err error

	var parentsLength uint64
	if err = binary.Read(r, binary.LittleEndian, &parentsLength); err != nil {
		return out, fmt.Errorf("failed to read Parents length: %w", err)
	}
	out.Parents = make([]H, parentsLength)
	for i := uint64(0); i < parentsLength; i++ {
		if out.Parents[i], err = DecodeHasherDomain[H](r); err != nil {
			return out, fmt.Errorf("failed to decode Parent: %w", err)
		}
	}

	if err = binary.Read(r, binary.LittleEndian, &out.LayerIndex); err != nil {
		return out, fmt.Errorf("failed to decode LayerIndex: %w", err)
	}

	if err = binary.Read(r, binary.LittleEndian, &out.Node); err != nil {
		return out, fmt.Errorf("failed to decode Node: %w", err)
	}

	return out, nil
}

func DecodeEncodingProof[H HasherDomain](r io.Reader) (EncodingProof[H], error) {
	var out EncodingProof[H]
	var err error

	var parentsLength uint64
	if err = binary.Read(r, binary.LittleEndian, &parentsLength); err != nil {
		return out, fmt.Errorf("failed to read Parents length: %w", err)
	}
	out.Parents = make([]H, parentsLength)
	for i := uint64(0); i < parentsLength; i++ {
		if out.Parents[i], err = DecodeHasherDomain[H](r); err != nil {
			return out, fmt.Errorf("failed to decode Parent: %w", err)
		}
	}

	if err = binary.Read(r, binary.LittleEndian, &out.LayerIndex); err != nil {
		return out, fmt.Errorf("failed to decode LayerIndex: %w", err)
	}

	if err = binary.Read(r, binary.LittleEndian, &out.Node); err != nil {
		return out, fmt.Errorf("failed to decode Node: %w", err)
	}

	return out, nil
}

func DecodeHasherDomain[H HasherDomain](r io.Reader) (H, error) {
	var out H
	if err := binary.Read(r, binary.LittleEndian, &out); err != nil {
		return out, fmt.Errorf("failed to decode HasherDomain: %w", err)
	}
	return out, nil
}

func DecodeTicket(r io.Reader) (Ticket, error) {
	var out Ticket
	if _, err := io.ReadFull(r, out[:]); err != nil {
		return out, fmt.Errorf("failed to decode Ticket: %w", err)
	}
	return out, nil
}

func DecodeCommitment(r io.Reader) (Commitment, error) {
	var out Commitment
	if _, err := io.ReadFull(r, out[:]); err != nil {
		return out, fmt.Errorf("failed to decode Commitment: %w", err)
	}
	return out, nil
}
