package proof

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/filecoin-project/go-state-types/abi"
)

// EncodeCommit1OutRaw serializes Commit1OutRaw into w in the same bincode style
// that DecodeCommit1OutRaw expects.
func EncodeCommit1OutRaw(w io.Writer, c Commit1OutRaw) error {
	// The decoder sets out.RegisteredProof = "StackedDrg32GiBV1_1"
	// but does NOT read that from the stream. We won't write it either.

	// 1) VanillaProofs
	//    The decoder expects only "StackedDrg32GiBV1" in the map, with structure:
	//    - outerLength (u64)
	//       for each outer slot:
	//         - innerLength (u64)
	//         for each inner slot: VanillaStackedProof
	proofs, ok := c.VanillaProofs["StackedDrg32GiBV1"]
	if !ok {
		// If there's no "StackedDrg32GiBV1" key, we can encode zero slices,
		// or we might decide to error. Here we encode zero slices to match decode logic.
		if err := WriteLE[uint64](w, 0); err != nil {
			return fmt.Errorf("writing 0 for proofs outer length: %w", err)
		}
	} else {
		// outer length
		if err := WriteLE[uint64](w, uint64(len(proofs))); err != nil {
			return fmt.Errorf("writing proofs outer length: %w", err)
		}
		for _, innerSlice := range proofs {
			// inner length
			if err := WriteLE[uint64](w, uint64(len(innerSlice))); err != nil {
				return fmt.Errorf("writing proofs inner length: %w", err)
			}
			for _, p := range innerSlice {
				if err := EncodeVanillaStackedProof(w, p); err != nil {
					return fmt.Errorf("encode vanilla stacked proof: %w", err)
				}
			}
		}
	}

	// 2) CommR
	if err := EncodeCommitment(w, c.CommR); err != nil {
		return fmt.Errorf("encode CommR: %w", err)
	}

	// 3) CommD
	if err := EncodeCommitment(w, c.CommD); err != nil {
		return fmt.Errorf("encode CommD: %w", err)
	}

	// 4) ReplicaID
	if err := EncodeCommitment(w, c.ReplicaID); err != nil {
		return fmt.Errorf("encode ReplicaID: %w", err)
	}

	// 5) Seed
	if err := EncodeTicket(w, c.Seed); err != nil {
		return fmt.Errorf("encode Seed: %w", err)
	}

	// 6) Ticket
	if err := EncodeTicket(w, c.Ticket); err != nil {
		return fmt.Errorf("encode Ticket: %w", err)
	}

	// The decoder expects EOF next, so don't write anything more.

	return nil
}

// EncodeVanillaStackedProof serializes a single VanillaStackedProof.
func EncodeVanillaStackedProof(w io.Writer, v VanillaStackedProof) error {
	// comm_d_proofs => MerkleProof[Sha256Domain]
	if err := EncodeMerkleProof[Sha256Domain](w, v.CommDProofs); err != nil {
		return fmt.Errorf("encode CommDProofs: %w", err)
	}
	// comm_r_last_proof => MerkleProof[PoseidonDomain]
	if err := EncodeMerkleProof[PoseidonDomain](w, v.CommRLastProof); err != nil {
		return fmt.Errorf("encode CommRLastProof: %w", err)
	}
	// replica_column_proofs
	if err := EncodeReplicaColumnProof[PoseidonDomain](w, v.ReplicaColumnProofs); err != nil {
		return fmt.Errorf("encode ReplicaColumnProofs: %w", err)
	}

	// labeling proofs
	if err := WriteLE[uint64](w, uint64(len(v.LabelingProofs))); err != nil {
		return fmt.Errorf("writing labelingProofs length: %w", err)
	}
	for _, lp := range v.LabelingProofs {
		if err := EncodeLabelingProof[PoseidonDomain](w, lp); err != nil {
			return fmt.Errorf("encode labeling proof: %w", err)
		}
	}

	// encoding proof
	if err := EncodeEncodingProof[PoseidonDomain](w, v.EncodingProof); err != nil {
		return fmt.Errorf("encode encoding proof: %w", err)
	}

	return nil
}

// EncodeMerkleProof writes a MerkleProof of type H.
func EncodeMerkleProof[H HasherDomain](w io.Writer, m MerkleProof[H]) error {
	// MerkleProof -> { Data: ProofData[H] }
	return EncodeProofData[H](w, m.Data)
}

// EncodeProofData checks Single, Sub, or Top and writes the matching tag + data.
func EncodeProofData[H HasherDomain](w io.Writer, pd ProofData[H]) error {
	switch {
	case pd.Single != nil:
		// Single => tag=0
		if err := WriteLE[uint32](w, 0); err != nil {
			return fmt.Errorf("write Single tag: %w", err)
		}
		if err := EncodeSingleProof(w, *pd.Single); err != nil {
			return fmt.Errorf("encode single proof: %w", err)
		}
	case pd.Sub != nil:
		// Sub => tag=1
		if err := WriteLE[uint32](w, 1); err != nil {
			return fmt.Errorf("write Sub tag: %w", err)
		}
		if err := EncodeSubProof(w, *pd.Sub); err != nil {
			return fmt.Errorf("encode sub proof: %w", err)
		}
	case pd.Top != nil:
		// Top => tag=2
		if err := WriteLE[uint32](w, 2); err != nil {
			return fmt.Errorf("write Top tag: %w", err)
		}
		if err := EncodeTopProof(w, *pd.Top); err != nil {
			return fmt.Errorf("encode top proof: %w", err)
		}
	default:
		return fmt.Errorf("proof data is nil for Single, Sub, and Top")
	}
	return nil
}

func EncodeSingleProof[H HasherDomain](w io.Writer, sp SingleProof[H]) error {
	if err := EncodeHasherDomain[H](w, sp.Root); err != nil {
		return fmt.Errorf("encode SingleProof Root: %w", err)
	}
	if err := EncodeHasherDomain[H](w, sp.Leaf); err != nil {
		return fmt.Errorf("encode SingleProof Leaf: %w", err)
	}
	if err := EncodeInclusionPath[H](w, sp.Path); err != nil {
		return fmt.Errorf("encode SingleProof Path: %w", err)
	}
	return nil
}

func EncodeSubProof[H HasherDomain](w io.Writer, sp SubProof[H]) error {
	if err := EncodeInclusionPath[H](w, sp.BaseProof); err != nil {
		return fmt.Errorf("encode SubProof BaseProof: %w", err)
	}
	if err := EncodeInclusionPath[H](w, sp.SubProof); err != nil {
		return fmt.Errorf("encode SubProof SubProof: %w", err)
	}
	if err := EncodeHasherDomain[H](w, sp.Root); err != nil {
		return fmt.Errorf("encode SubProof Root: %w", err)
	}
	if err := EncodeHasherDomain[H](w, sp.Leaf); err != nil {
		return fmt.Errorf("encode SubProof Leaf: %w", err)
	}
	return nil
}

func EncodeTopProof[H HasherDomain](w io.Writer, tp TopProof[H]) error {
	if err := EncodeInclusionPath[H](w, tp.BaseProof); err != nil {
		return fmt.Errorf("encode TopProof BaseProof: %w", err)
	}
	if err := EncodeInclusionPath[H](w, tp.SubProof); err != nil {
		return fmt.Errorf("encode TopProof SubProof: %w", err)
	}
	if err := EncodeInclusionPath[H](w, tp.TopProof); err != nil {
		return fmt.Errorf("encode TopProof TopProof: %w", err)
	}
	if err := EncodeHasherDomain[H](w, tp.Root); err != nil {
		return fmt.Errorf("encode TopProof Root: %w", err)
	}
	if err := EncodeHasherDomain[H](w, tp.Leaf); err != nil {
		return fmt.Errorf("encode TopProof Leaf: %w", err)
	}
	return nil
}

func EncodeInclusionPath[H HasherDomain](w io.Writer, ip InclusionPath[H]) error {
	// first write path length
	if err := WriteLE[uint64](w, uint64(len(ip.Path))); err != nil {
		return fmt.Errorf("writing path length: %w", err)
	}
	for _, el := range ip.Path {
		if err := EncodePathElement[H](w, el); err != nil {
			return fmt.Errorf("encode path element: %w", err)
		}
	}
	return nil
}

func EncodePathElement[H HasherDomain](w io.Writer, pe PathElement[H]) error {
	// first write number of hashes
	if err := WriteLE[uint64](w, uint64(len(pe.Hashes))); err != nil {
		return fmt.Errorf("writing number of path-element hashes: %w", err)
	}
	for _, h := range pe.Hashes {
		if err := EncodeHasherDomain[H](w, h); err != nil {
			return fmt.Errorf("encode path-element hash: %w", err)
		}
	}
	// then index
	if err := WriteLE[uint64](w, pe.Index); err != nil {
		return fmt.Errorf("writing path-element index: %w", err)
	}

	return nil
}

func EncodeReplicaColumnProof[H HasherDomain](w io.Writer, rcp ReplicaColumnProof[H]) error {
	// c_x
	if err := EncodeColumnProof[H](w, rcp.C_X); err != nil {
		return fmt.Errorf("encode c_x: %w", err)
	}

	// drg_parents
	if err := WriteLE[uint64](w, uint64(len(rcp.DrgParents))); err != nil {
		return fmt.Errorf("writing drg_parents length: %w", err)
	}
	for _, cp := range rcp.DrgParents {
		if err := EncodeColumnProof[H](w, cp); err != nil {
			return fmt.Errorf("encode drg parent: %w", err)
		}
	}

	// exp_parents
	if err := WriteLE[uint64](w, uint64(len(rcp.ExpParents))); err != nil {
		return fmt.Errorf("writing exp_parents length: %w", err)
	}
	for _, cp := range rcp.ExpParents {
		if err := EncodeColumnProof[H](w, cp); err != nil {
			return fmt.Errorf("encode exp parent: %w", err)
		}
	}

	return nil
}

func EncodeColumnProof[H HasherDomain](w io.Writer, cp ColumnProof[H]) error {
	if err := EncodeColumn[H](w, cp.Column); err != nil {
		return fmt.Errorf("encode column: %w", err)
	}
	if err := EncodeMerkleProof[H](w, cp.InclusionProof); err != nil {
		return fmt.Errorf("encode inclusion proof: %w", err)
	}
	return nil
}

func EncodeColumn[H HasherDomain](w io.Writer, c Column[H]) error {
	// index (u32)
	if err := WriteLE[uint32](w, c.Index); err != nil {
		return fmt.Errorf("writing column index: %w", err)
	}

	// rows slice length (u64)
	if err := WriteLE[uint64](w, uint64(len(c.Rows))); err != nil {
		return fmt.Errorf("writing column rows length: %w", err)
	}
	for _, row := range c.Rows {
		if err := EncodeHasherDomain[H](w, row); err != nil {
			return fmt.Errorf("encode column row: %w", err)
		}
	}

	// The `_h` field is skipped in decode => skip in encode too
	return nil
}

func EncodeLabelingProof[H HasherDomain](w io.Writer, lp LabelingProof[H]) error {
	// parents length
	if err := WriteLE[uint64](w, uint64(len(lp.Parents))); err != nil {
		return fmt.Errorf("writing labeling proof parents length: %w", err)
	}
	for _, p := range lp.Parents {
		if err := EncodeHasherDomain[H](w, p); err != nil {
			return fmt.Errorf("encode labeling proof parent: %w", err)
		}
	}

	// layer_index (u32)
	if err := WriteLE[uint32](w, lp.LayerIndex); err != nil {
		return fmt.Errorf("writing labeling proof layer_index: %w", err)
	}

	// node (u64)
	if err := WriteLE[uint64](w, lp.Node); err != nil {
		return fmt.Errorf("writing labeling proof node: %w", err)
	}

	return nil
}

func EncodeEncodingProof[H HasherDomain](w io.Writer, ep EncodingProof[H]) error {
	// parents length
	if err := WriteLE[uint64](w, uint64(len(ep.Parents))); err != nil {
		return fmt.Errorf("writing encoding proof parents length: %w", err)
	}
	for _, p := range ep.Parents {
		if err := EncodeHasherDomain[H](w, p); err != nil {
			return fmt.Errorf("encode encoding proof parent: %w", err)
		}
	}

	// layer_index (u32)
	if err := WriteLE[uint32](w, ep.LayerIndex); err != nil {
		return fmt.Errorf("writing encoding proof layer_index: %w", err)
	}

	// node (u64)
	if err := WriteLE[uint64](w, ep.Node); err != nil {
		return fmt.Errorf("writing encoding proof node: %w", err)
	}

	return nil
}

// EncodeHasherDomain writes a HasherDomain (e.g. [32]byte) in LE order
// matching the decode.  For a [32]byte it’s just 32 raw bytes, no reordering.
func EncodeHasherDomain[H HasherDomain](w io.Writer, h H) error {
	if err := binary.Write(w, binary.LittleEndian, &h); err != nil {
		return fmt.Errorf("failed to encode hasher domain: %w", err)
	}
	return nil
}

// EncodeTicket is simply 32 raw bytes. Must match decoding logic.
func EncodeTicket(w io.Writer, t Ticket) error {
	if _, err := w.Write(t[:]); err != nil {
		return fmt.Errorf("writing ticket: %w", err)
	}
	return nil
}

// EncodeCommitment is likewise just 32 raw bytes. Must match decoding logic.
func EncodeCommitment(w io.Writer, c Commitment) error {
	if _, err := w.Write(c[:]); err != nil {
		return fmt.Errorf("writing commitment: %w", err)
	}
	return nil
}

// cbor marshal to bincoded cborbytes
func (c *Commit1OutRaw) MarshalCBOR(w io.Writer) error {
	var buf bytes.Buffer
	err := EncodeCommit1OutRaw(&buf, *c)
	if err != nil {
		return err
	}

	var cbb abi.CborBytes = buf.Bytes()
	return cbb.MarshalCBOR(w)
}
