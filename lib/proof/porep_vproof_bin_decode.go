package proof

import (
	"encoding/binary"
	"fmt"
	"io"
)

func DecodeCommit1OutRaw(r io.Reader) (Commit1OutRaw, error) {
	var out Commit1OutRaw
	var err error

	if out.CommD, err = DecodeCommitment(r); err != nil {
		return out, err
	}
	if out.CommR, err = DecodeCommitment(r); err != nil {
		return out, err
	}
	if out.RegisteredProof, err = DecodeStringRegisteredProofType(r); err != nil {
		return out, err
	}
	if out.ReplicaID, err = DecodeCommitment(r); err != nil {
		return out, err
	}
	if out.Seed, err = DecodeTicket(r); err != nil {
		return out, err
	}
	if out.Ticket, err = DecodeTicket(r); err != nil {
		return out, err
	}

	// Decode VanillaProofs
	numProofTypes, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}

	out.VanillaProofs = make(map[StringRegisteredProofType][][]VanillaStackedProof)
	for i := uint64(0); i < numProofTypes; i++ {
		proofType, err := DecodeStringRegisteredProofType(r)
		if err != nil {
			return out, err
		}

		numPartitions, err := binary.ReadUvarint(r.(io.ByteReader))
		if err != nil {
			return out, err
		}

		partitions := make([][]VanillaStackedProof, numPartitions)
		for j := uint64(0); j < numPartitions; j++ {
			numChallenges, err := binary.ReadUvarint(r.(io.ByteReader))
			if err != nil {
				return out, err
			}

			challenges := make([]VanillaStackedProof, numChallenges)
			for k := uint64(0); k < numChallenges; k++ {
				challenges[k], err = DecodeVanillaStackedProof(r)
				if err != nil {
					return out, err
				}
			}
			partitions[j] = challenges
		}
		out.VanillaProofs[proofType] = partitions
	}

	return out, nil
}

func DecodeVanillaStackedProof(r io.Reader) (VanillaStackedProof, error) {
	var out VanillaStackedProof
	var err error

	if out.CommDProofs, err = DecodeMerkleProof[Sha256Domain](r); err != nil {
		return out, err
	}
	if out.CommRLastProof, err = DecodeMerkleProof[PoseidonDomain](r); err != nil {
		return out, err
	}
	if out.ReplicaColumnProofs, err = DecodeReplicaColumnProof[PoseidonDomain](r); err != nil {
		return out, err
	}

	numLabelingProofs, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.LabelingProofs = make([]LabelingProof[PoseidonDomain], numLabelingProofs)
	for i := uint64(0); i < numLabelingProofs; i++ {
		out.LabelingProofs[i], err = DecodeLabelingProof[PoseidonDomain](r)
		if err != nil {
			return out, err
		}
	}

	out.EncodingProof, err = DecodeEncodingProof[PoseidonDomain](r)
	if err != nil {
		return out, err
	}

	return out, nil
}

func DecodeMerkleProof[H HasherDomain](r io.Reader) (MerkleProof[H], error) {
	var out MerkleProof[H]
	var err error

	out.Data, err = DecodeProofData[H](r)
	if err != nil {
		return out, err
	}

	return out, nil
}

func DecodeProofData[H HasherDomain](r io.Reader) (ProofData[H], error) {
	var out ProofData[H]
	var err error

	proofType, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
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

	return out, err
}

func DecodeSingleProof[H HasherDomain](r io.Reader) (SingleProof[H], error) {
	var out SingleProof[H]
	var err error

	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}
	if out.Path, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeSubProof[H HasherDomain](r io.Reader) (SubProof[H], error) {
	var out SubProof[H]
	var err error

	if out.BaseProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}
	if out.SubProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}
	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeTopProof[H HasherDomain](r io.Reader) (TopProof[H], error) {
	var out TopProof[H]
	var err error

	if out.BaseProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}
	if out.SubProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}
	if out.TopProof, err = DecodeInclusionPath[H](r); err != nil {
		return out, err
	}
	if out.Root, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}
	if out.Leaf, err = DecodeHasherDomain[H](r); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeInclusionPath[H HasherDomain](r io.Reader) (InclusionPath[H], error) {
	var out InclusionPath[H]
	var err error

	numElements, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}

	out.Path = make([]PathElement[H], numElements)
	for i := uint64(0); i < numElements; i++ {
		out.Path[i], err = DecodePathElement[H](r)
		if err != nil {
			return out, err
		}
	}

	return out, nil
}

func DecodePathElement[H HasherDomain](r io.Reader) (PathElement[H], error) {
	var out PathElement[H]
	var err error

	numHashes, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}

	out.Hashes = make([]H, numHashes)
	for i := uint64(0); i < numHashes; i++ {
		out.Hashes[i], err = DecodeHasherDomain[H](r)
		if err != nil {
			return out, err
		}
	}

	out.Index, err = binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}

	return out, nil
}

func DecodeReplicaColumnProof[H HasherDomain](r io.Reader) (ReplicaColumnProof[H], error) {
	var out ReplicaColumnProof[H]
	var err error

	if out.C_X, err = DecodeColumnProof[H](r); err != nil {
		return out, err
	}

	numDrgParents, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.DrgParents = make([]ColumnProof[H], numDrgParents)
	for i := uint64(0); i < numDrgParents; i++ {
		out.DrgParents[i], err = DecodeColumnProof[H](r)
		if err != nil {
			return out, err
		}
	}

	numExpParents, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.ExpParents = make([]ColumnProof[H], numExpParents)
	for i := uint64(0); i < numExpParents; i++ {
		out.ExpParents[i], err = DecodeColumnProof[H](r)
		if err != nil {
			return out, err
		}
	}

	return out, nil
}

func DecodeColumnProof[H HasherDomain](r io.Reader) (ColumnProof[H], error) {
	var out ColumnProof[H]
	var err error

	if out.Column, err = DecodeColumn[H](r); err != nil {
		return out, err
	}
	if out.InclusionProof, err = DecodeMerkleProof[H](r); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeColumn[H HasherDomain](r io.Reader) (Column[H], error) {
	var out Column[H]
	var err error

	if err = binary.Read(r, binary.LittleEndian, &out.Index); err != nil {
		return out, err
	}

	numRows, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.Rows = make([]H, numRows)
	for i := uint64(0); i < numRows; i++ {
		out.Rows[i], err = DecodeHasherDomain[H](r)
		if err != nil {
			return out, err
		}
	}

	return out, nil
}

func DecodeLabelingProof[H HasherDomain](r io.Reader) (LabelingProof[H], error) {
	var out LabelingProof[H]
	var err error

	numParents, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.Parents = make([]H, numParents)
	for i := uint64(0); i < numParents; i++ {
		out.Parents[i], err = DecodeHasherDomain[H](r)
		if err != nil {
			return out, err
		}
	}

	if err = binary.Read(r, binary.LittleEndian, &out.LayerIndex); err != nil {
		return out, err
	}
	if err = binary.Read(r, binary.LittleEndian, &out.Node); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeEncodingProof[H HasherDomain](r io.Reader) (EncodingProof[H], error) {
	var out EncodingProof[H]
	var err error

	numParents, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return out, err
	}
	out.Parents = make([]H, numParents)
	for i := uint64(0); i < numParents; i++ {
		out.Parents[i], err = DecodeHasherDomain[H](r)
		if err != nil {
			return out, err
		}
	}

	if err = binary.Read(r, binary.LittleEndian, &out.LayerIndex); err != nil {
		return out, err
	}
	if err = binary.Read(r, binary.LittleEndian, &out.Node); err != nil {
		return out, err
	}

	return out, nil
}

func DecodeCommitment(r io.Reader) (Commitment, error) {
	var out Commitment
	_, err := io.ReadFull(r, out[:])
	return out, err
}

func DecodeTicket(r io.Reader) (Ticket, error) {
	var out Ticket
	_, err := io.ReadFull(r, out[:])
	return out, err
}

func DecodeStringRegisteredProofType(r io.Reader) (StringRegisteredProofType, error) {
	length, err := binary.ReadUvarint(r.(io.ByteReader))
	if err != nil {
		return "", err
	}

	bytes := make([]byte, length)
	_, err = io.ReadFull(r, bytes)
	if err != nil {
		return "", err
	}

	return StringRegisteredProofType(bytes), nil
}

func DecodeHasherDomain[H HasherDomain](r io.Reader) (H, error) {
	var out H
	err := binary.Read(r, binary.LittleEndian, &out)
	return out, err
}
