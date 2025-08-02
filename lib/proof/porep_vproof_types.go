package proof

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
)

// This file contains PoRep vanilla proof type definitions from
// - https://github.com/filecoin-project/rust-fil-proofs/tree/master/storage-proofs-core/src/merkle
// - https://github.com/filecoin-project/rust-fil-proofs/tree/master/storage-proofs-porep/src/stacked/vanilla
// - https://github.com/filecoin-project/rust-filecoin-proofs-api/tree/master/src
// The json representation of those matches the representation expected by rust-fil-proofs.

// core

type Commitment [32]byte
type Ticket [32]byte

type StringRegisteredProofType string // e.g. "StackedDrg2KiBV1", StackedDrg32GiBV1_1

/*
// These enumerations must match the proofs library and never change.
type RegisteredSealProof int64

const (

	RegisteredSealProof_StackedDrg2KiBV1   = RegisteredSealProof(0)
	RegisteredSealProof_StackedDrg8MiBV1   = RegisteredSealProof(1)
	RegisteredSealProof_StackedDrg512MiBV1 = RegisteredSealProof(2)
	RegisteredSealProof_StackedDrg32GiBV1  = RegisteredSealProof(3)
	RegisteredSealProof_StackedDrg64GiBV1  = RegisteredSealProof(4)

	RegisteredSealProof_StackedDrg2KiBV1_1   = RegisteredSealProof(5)
	RegisteredSealProof_StackedDrg8MiBV1_1   = RegisteredSealProof(6)
	RegisteredSealProof_StackedDrg512MiBV1_1 = RegisteredSealProof(7)
	RegisteredSealProof_StackedDrg32GiBV1_1  = RegisteredSealProof(8)
	RegisteredSealProof_StackedDrg64GiBV1_1  = RegisteredSealProof(9)

	RegisteredSealProof_StackedDrg2KiBV1_1_Feat_SyntheticPoRep   = RegisteredSealProof(10)
	RegisteredSealProof_StackedDrg8MiBV1_1_Feat_SyntheticPoRep   = RegisteredSealProof(11)
	RegisteredSealProof_StackedDrg512MiBV1_1_Feat_SyntheticPoRep = RegisteredSealProof(12)
	RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep  = RegisteredSealProof(13)
	RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep  = RegisteredSealProof(14)

	RegisteredSealProof_StackedDrg2KiBV1_2_Feat_NiPoRep   = RegisteredSealProof(15)
	RegisteredSealProof_StackedDrg8MiBV1_2_Feat_NiPoRep   = RegisteredSealProof(16)
	RegisteredSealProof_StackedDrg512MiBV1_2_Feat_NiPoRep = RegisteredSealProof(17)
	RegisteredSealProof_StackedDrg32GiBV1_2_Feat_NiPoRep  = RegisteredSealProof(18)
	RegisteredSealProof_StackedDrg64GiBV1_2_Feat_NiPoRep  = RegisteredSealProof(19)

)

Rust:
/// Available seal proofs.
// Enum is append-only: once published, a `RegisteredSealProof` value must never change.
#[allow(non_camel_case_types)]
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]

	pub enum RegisteredSealProof {
	    StackedDrg2KiBV1,
	    StackedDrg8MiBV1,
	    StackedDrg512MiBV1,
	    StackedDrg32GiBV1,
	    StackedDrg64GiBV1,

	    StackedDrg2KiBV1_1,
	    StackedDrg8MiBV1_1,
	    StackedDrg512MiBV1_1,
	    StackedDrg32GiBV1_1,
	    StackedDrg64GiBV1_1,

	    StackedDrg2KiBV1_1_Feat_SyntheticPoRep,
	    StackedDrg8MiBV1_1_Feat_SyntheticPoRep,
	    StackedDrg512MiBV1_1_Feat_SyntheticPoRep,
	    StackedDrg32GiBV1_1_Feat_SyntheticPoRep,
	    StackedDrg64GiBV1_1_Feat_SyntheticPoRep,

	    // NOTE: The SyntheticPoRep feature was added in proofs API
	    // version 1.2, however the published proof name has the incorrect
	    // version 1_1 coded into it.
	    //
	    // Non-interactive PoRep is also a feature added at API version
	    // 1.2, so the naming has been corrected before publication.
	    StackedDrg2KiBV1_2_Feat_NonInteractivePoRep,
	    StackedDrg8MiBV1_2_Feat_NonInteractivePoRep,
	    StackedDrg512MiBV1_2_Feat_NonInteractivePoRep,
	    StackedDrg32GiBV1_2_Feat_NonInteractivePoRep,
	    StackedDrg64GiBV1_2_Feat_NonInteractivePoRep,
	}
*/
func (s StringRegisteredProofType) ToABI() (abi.RegisteredSealProof, error) {
	switch s {
	case "StackedDrg2KiBV1":
		return abi.RegisteredSealProof_StackedDrg2KiBV1, nil
	case "StackedDrg8MiBV1":
		return abi.RegisteredSealProof_StackedDrg8MiBV1, nil
	case "StackedDrg512MiBV1":
		return abi.RegisteredSealProof_StackedDrg512MiBV1, nil
	case "StackedDrg32GiBV1":
		return abi.RegisteredSealProof_StackedDrg32GiBV1, nil
	case "StackedDrg64GiBV1":
		return abi.RegisteredSealProof_StackedDrg64GiBV1, nil

	case "StackedDrg2KiBV1_1":
		return abi.RegisteredSealProof_StackedDrg2KiBV1_1, nil
	case "StackedDrg8MiBV1_1":
		return abi.RegisteredSealProof_StackedDrg8MiBV1_1, nil
	case "StackedDrg512MiBV1_1":
		return abi.RegisteredSealProof_StackedDrg512MiBV1_1, nil
	case "StackedDrg32GiBV1_1":
		return abi.RegisteredSealProof_StackedDrg32GiBV1_1, nil
	case "StackedDrg64GiBV1_1":
		return abi.RegisteredSealProof_StackedDrg64GiBV1_1, nil

	case "StackedDrg2KiBV1_1_Feat_SyntheticPoRep":
		return abi.RegisteredSealProof_StackedDrg2KiBV1_1_Feat_SyntheticPoRep, nil
	case "StackedDrg8MiBV1_1_Feat_SyntheticPoRep":
		return abi.RegisteredSealProof_StackedDrg8MiBV1_1_Feat_SyntheticPoRep, nil
	case "StackedDrg512MiBV1_1_Feat_SyntheticPoRep":
		return abi.RegisteredSealProof_StackedDrg512MiBV1_1_Feat_SyntheticPoRep, nil
	case "StackedDrg32GiBV1_1_Feat_SyntheticPoRep":
		return abi.RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep, nil
	case "StackedDrg64GiBV1_1_Feat_SyntheticPoRep":
		return abi.RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep, nil

	case "StackedDrg2KiBV1_2_Feat_NonInteractivePoRep":
		return abi.RegisteredSealProof_StackedDrg2KiBV1_2_Feat_NiPoRep, nil
	case "StackedDrg8MiBV1_2_Feat_NonInteractivePoRep":
		return abi.RegisteredSealProof_StackedDrg8MiBV1_2_Feat_NiPoRep, nil
	case "StackedDrg512MiBV1_2_Feat_NonInteractivePoRep":
		return abi.RegisteredSealProof_StackedDrg512MiBV1_2_Feat_NiPoRep, nil
	case "StackedDrg32GiBV1_2_Feat_NonInteractivePoRep":
		return abi.RegisteredSealProof_StackedDrg32GiBV1_2_Feat_NiPoRep, nil
	case "StackedDrg64GiBV1_2_Feat_NonInteractivePoRep":
		return abi.RegisteredSealProof_StackedDrg64GiBV1_2_Feat_NiPoRep, nil

	default:
		return 0, xerrors.Errorf("unknown proof type: %s", s)
	}
}

func StringProofFromAbi(p abi.RegisteredSealProof) (string, error) {
	switch p {
	case abi.RegisteredSealProof_StackedDrg2KiBV1:
		return "StackedDrg2KiBV1", nil
	case abi.RegisteredSealProof_StackedDrg8MiBV1:
		return "StackedDrg8MiBV1", nil
	case abi.RegisteredSealProof_StackedDrg512MiBV1:
		return "StackedDrg512MiBV1", nil
	case abi.RegisteredSealProof_StackedDrg32GiBV1:
		return "StackedDrg32GiBV1", nil
	case abi.RegisteredSealProof_StackedDrg64GiBV1:
		return "StackedDrg64GiBV1", nil

	case abi.RegisteredSealProof_StackedDrg2KiBV1_1:
		return "StackedDrg2KiBV1_1", nil
	case abi.RegisteredSealProof_StackedDrg8MiBV1_1:
		return "StackedDrg8MiBV1_1", nil
	case abi.RegisteredSealProof_StackedDrg512MiBV1_1:
		return "StackedDrg512MiBV1_1", nil
	case abi.RegisteredSealProof_StackedDrg32GiBV1_1:
		return "StackedDrg32GiBV1_1", nil
	case abi.RegisteredSealProof_StackedDrg64GiBV1_1:
		return "StackedDrg64GiBV1_1", nil

	case abi.RegisteredSealProof_StackedDrg2KiBV1_1_Feat_SyntheticPoRep:
		return "StackedDrg2KiBV1_1_Feat_SyntheticPoRep", nil
	case abi.RegisteredSealProof_StackedDrg8MiBV1_1_Feat_SyntheticPoRep:
		return "StackedDrg8MiBV1_1_Feat_SyntheticPoRep", nil
	case abi.RegisteredSealProof_StackedDrg512MiBV1_1_Feat_SyntheticPoRep:
		return "StackedDrg512MiBV1_1_Feat_SyntheticPoRep", nil
	case abi.RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep:
		return "StackedDrg32GiBV1_1_Feat_SyntheticPoRep", nil
	case abi.RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep:
		return "StackedDrg64GiBV1_1_Feat_SyntheticPoRep", nil

	case abi.RegisteredSealProof_StackedDrg2KiBV1_2_Feat_NiPoRep:
		return "StackedDrg2KiBV1_2_Feat_NonInteractivePoRep", nil
	case abi.RegisteredSealProof_StackedDrg8MiBV1_2_Feat_NiPoRep:
		return "StackedDrg8MiBV1_2_Feat_NonInteractivePoRep", nil
	case abi.RegisteredSealProof_StackedDrg512MiBV1_2_Feat_NiPoRep:
		return "StackedDrg512MiBV1_2_Feat_NonInteractivePoRep", nil
	case abi.RegisteredSealProof_StackedDrg32GiBV1_2_Feat_NiPoRep:
		return "StackedDrg32GiBV1_2_Feat_NonInteractivePoRep", nil
	case abi.RegisteredSealProof_StackedDrg64GiBV1_2_Feat_NiPoRep:
		return "StackedDrg64GiBV1_2_Feat_NonInteractivePoRep", nil

	default:
		return "", xerrors.Errorf("unknown proof type: %d", p)
	}
}

// porep

type Label struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	RowsToDiscard int    `json:"rows_to_discard"`
	Size          int    `json:"size"`
}

type Labels struct {
	H      any     `json:"_h"` // todo ?
	Labels []Label `json:"labels"`
}

type PreCommit1OutRaw struct {
	LotusSealRand []byte `json:"_lotus_SealRandomness"`

	CommD           Commitment                           `json:"comm_d"`
	Config          Label                                `json:"config"`
	Labels          map[StringRegisteredProofType]Labels `json:"labels"`
	RegisteredProof StringRegisteredProofType            `json:"registered_proof"`
}

/* Commit1OutRaw maps to:
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SealCommitPhase1Output {
    pub registered_proof: RegisteredSealProof,
    pub vanilla_proofs: VanillaSealProof,
    pub comm_r: Commitment,
    pub comm_d: Commitment,
    pub replica_id: <filecoin_proofs_v1::constants::DefaultTreeHasher as Hasher>::Domain,
    pub seed: Ticket,
    pub ticket: Ticket,
}
*/

type Commit1OutRaw struct {
	CommD           Commitment                `json:"comm_d"`
	CommR           Commitment                `json:"comm_r"`
	RegisteredProof StringRegisteredProofType `json:"registered_proof"`
	ReplicaID       Commitment                `json:"replica_id"`
	Seed            Ticket                    `json:"seed"`
	Ticket          Ticket                    `json:"ticket"`

	// ProofType -> [partitions] -> [challenge_index?] -> Proof
	VanillaProofs map[StringRegisteredProofType][][]VanillaStackedProof `json:"vanilla_proofs"`
}

/*
#[derive(Clone, Debug, Serialize, Deserialize)]
pub enum VanillaSealProof {
    StackedDrg2KiBV1(Vec<Vec<RawVanillaSealProof<SectorShape2KiB>>>),
    StackedDrg8MiBV1(Vec<Vec<RawVanillaSealProof<SectorShape8MiB>>>),
    StackedDrg512MiBV1(Vec<Vec<RawVanillaSealProof<SectorShape512MiB>>>),
    StackedDrg32GiBV1(Vec<Vec<RawVanillaSealProof<SectorShape32GiB>>>),
    StackedDrg64GiBV1(Vec<Vec<RawVanillaSealProof<SectorShape64GiB>>>),
}

//VanillaSealProof as RawVanillaSealProof
pub type VanillaSealProof<Tree> = stacked::Proof<Tree, DefaultPieceHasher>;

#[derive(Debug, Serialize, Deserialize)]
pub struct Proof<Tree: MerkleTreeTrait, G: Hasher> {
    #[serde(bound(
        serialize = "MerkleProof<G, U2>: Serialize",
        deserialize = "MerkleProof<G, U2>: Deserialize<'de>"
    ))]
    pub comm_d_proofs: MerkleProof<G, U2>,
    #[serde(bound(
        serialize = "MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>: Serialize",
        deserialize = "MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>: Deserialize<'de>"
    ))]
    pub comm_r_last_proof:
        MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>,
    #[serde(bound(
        serialize = "ReplicaColumnProof<MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>,>: Serialize",
        deserialize = "ReplicaColumnProof<MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>>: Deserialize<'de>"
    ))]
    pub replica_column_proofs: ReplicaColumnProof<
        MerkleProof<Tree::Hasher, Tree::Arity, Tree::SubTreeArity, Tree::TopTreeArity>,
    >,
    #[serde(bound(
        serialize = "LabelingProof<Tree::Hasher>: Serialize",
        deserialize = "LabelingProof<Tree::Hasher>: Deserialize<'de>"
    ))]
    /// Indexed by layer in 1..layers.
    pub labeling_proofs: Vec<LabelingProof<Tree::Hasher>>,
    #[serde(bound(
        serialize = "EncodingProof<Tree::Hasher>: Serialize",
        deserialize = "EncodingProof<Tree::Hasher>: Deserialize<'de>"
    ))]
    pub encoding_proof: EncodingProof<Tree::Hasher>,
}

*/

type VanillaStackedProof struct {
	CommDProofs    MerkleProof[Sha256Domain]   `json:"comm_d_proofs"`
	CommRLastProof MerkleProof[PoseidonDomain] `json:"comm_r_last_proof"`

	ReplicaColumnProofs ReplicaColumnProof[PoseidonDomain] `json:"replica_column_proofs"`
	LabelingProofs      []LabelingProof[PoseidonDomain]    `json:"labeling_proofs"`
	EncodingProof       EncodingProof[PoseidonDomain]      `json:"encoding_proof"`
}

/*
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReplicaColumnProof<Proof: MerkleProofTrait> {
    #[serde(bound(
        serialize = "ColumnProof<Proof>: Serialize",
        deserialize = "ColumnProof<Proof>: Deserialize<'de>"
    ))]
    pub c_x: ColumnProof<Proof>,
    #[serde(bound(
        serialize = "ColumnProof<Proof>: Serialize",
        deserialize = "ColumnProof<Proof>: Deserialize<'de>"
    ))]
    pub drg_parents: Vec<ColumnProof<Proof>>,
    #[serde(bound(
        serialize = "ColumnProof<Proof>: Serialize",
        deserialize = "ColumnProof<Proof>: Deserialize<'de>"
    ))]
    pub exp_parents: Vec<ColumnProof<Proof>>,
}
*/

type ReplicaColumnProof[H HasherDomain] struct {
	C_X        ColumnProof[H]   `json:"c_x"`
	DrgParents []ColumnProof[H] `json:"drg_parents"`
	ExpParents []ColumnProof[H] `json:"exp_parents"`
}

/*
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ColumnProof<Proof: MerkleProofTrait> {
    #[serde(bound(
        serialize = "Column<Proof::Hasher>: Serialize",
        deserialize = "Column<Proof::Hasher>: Deserialize<'de>"
    ))]
    pub(crate) column: Column<Proof::Hasher>,
    #[serde(bound(
        serialize = "Proof: Serialize",
        deserialize = "Proof: DeserializeOwned"
    ))]
    pub(crate) inclusion_proof: Proof,
}
*/

type ColumnProof[H HasherDomain] struct {
	Column         Column[H]      `json:"column"`
	InclusionProof MerkleProof[H] `json:"inclusion_proof"`
}

/*
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct Column<H: Hasher> {
    pub(crate) index: u32,
    pub(crate) rows: Vec<H::Domain>,
    _h: PhantomData<H>,
}
*/

type Column[H HasherDomain] struct {
	Index uint32 `json:"index"`
	Rows  []H    `json:"rows"`
	H     any    `json:"_h"`
}

/*
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LabelingProof<H: Hasher> {
    pub(crate) parents: Vec<H::Domain>,
    pub(crate) layer_index: u32,
    pub(crate) node: u64,
    #[serde(skip)]
    _h: PhantomData<H>,
}
*/

type LabelingProof[H HasherDomain] struct {
	Parents    []H    `json:"parents"`
	LayerIndex uint32 `json:"layer_index"`
	Node       uint64 `json:"node"`
	//H          any    `json:"_h"`
}

/*
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EncodingProof<H: Hasher> {
    pub(crate) parents: Vec<H::Domain>,
    pub(crate) layer_index: u32,
    pub(crate) node: u64,
    #[serde(skip)]
    _h: PhantomData<H>,
}
*/

type EncodingProof[H HasherDomain] struct {
	Parents    []H    `json:"parents"`
	LayerIndex uint32 `json:"layer_index"`
	Node       uint64 `json:"node"`
	//H          any    `json:"_h"`
}

const NODE_SIZE = 32

// SectorNodes is sector size as node count
type SectorNodes uint64
