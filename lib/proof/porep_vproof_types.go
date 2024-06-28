package proof

// This file contains some type definitions from
// - https://github.com/filecoin-project/rust-fil-proofs/tree/master/storage-proofs-core/src/merkle
// - https://github.com/filecoin-project/rust-fil-proofs/tree/master/storage-proofs-porep/src/stacked/vanilla
// - https://github.com/filecoin-project/rust-filecoin-proofs-api/tree/master/src

// core

type Commitment [32]byte
type Ticket [32]byte

type StringRegisteredProofType string // e.g. "StackedDrg2KiBV1"

type HasherDomain = any

type Sha256Domain [32]byte

type PoseidonDomain [32]byte // Fr

/*
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct MerkleProof<
    H: Hasher,
    BaseArity: PoseidonArity,
    SubTreeArity: PoseidonArity = U0,
    TopTreeArity: PoseidonArity = U0,
> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    data: ProofData<H, BaseArity, SubTreeArity, TopTreeArity>,
}
*/

type MerkleProof[H HasherDomain] struct {
	Data ProofData[H] `json:"data"`
}

/*
#[derive(Debug, Clone, Serialize, Deserialize)]
enum ProofData<
    H: Hasher,
    BaseArity: PoseidonArity,
    SubTreeArity: PoseidonArity,
    TopTreeArity: PoseidonArity,
> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    Single(SingleProof<H, BaseArity>),
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    Sub(SubProof<H, BaseArity, SubTreeArity>),
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    Top(TopProof<H, BaseArity, SubTreeArity, TopTreeArity>),
}
*/

type ProofData[H HasherDomain] struct {
	Single *SingleProof[H] `json:"Single,omitempty"`
	Sub    *SubProof[H]    `json:"Sub,omitempty"`
	Top    *TopProof[H]    `json:"Top,omitempty"`
}

/*
struct SingleProof<H: Hasher, Arity: PoseidonArity> {
    /// Root of the merkle tree.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    root: H::Domain,
    /// The original leaf data for this prof.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    leaf: H::Domain,
    /// The path from leaf to root.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    path: InclusionPath<H, Arity>,
}
*/

type SingleProof[H HasherDomain] struct {
	Root H                `json:"root"`
	Leaf H                `json:"leaf"`
	Path InclusionPath[H] `json:"path"`
}

/*
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
struct SubProof<H: Hasher, BaseArity: PoseidonArity, SubTreeArity: PoseidonArity> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    base_proof: InclusionPath<H, BaseArity>,
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    sub_proof: InclusionPath<H, SubTreeArity>,
    /// Root of the merkle tree.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    root: H::Domain,
    /// The original leaf data for this prof.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    leaf: H::Domain,
}

*/

type SubProof[H HasherDomain] struct {
	BaseProof InclusionPath[H] `json:"base_proof"`
	SubProof  InclusionPath[H] `json:"sub_proof"`
	Root      H                `json:"root"`
	Leaf      H                `json:"leaf"`
}

/*
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
struct TopProof<
    H: Hasher,
    BaseArity: PoseidonArity,
    SubTreeArity: PoseidonArity,
    TopTreeArity: PoseidonArity,
> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    base_proof: InclusionPath<H, BaseArity>,
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    sub_proof: InclusionPath<H, SubTreeArity>,
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    top_proof: InclusionPath<H, TopTreeArity>,
    /// Root of the merkle tree.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    root: H::Domain,
    /// The original leaf data for this prof.
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    leaf: H::Domain,
}
*/

type TopProof[H HasherDomain] struct {
	BaseProof InclusionPath[H] `json:"base_proof"`
	SubProof  InclusionPath[H] `json:"sub_proof"`
	TopProof  InclusionPath[H] `json:"top_proof"`

	Root H `json:"root"`
	Leaf H `json:"leaf"`
}

/*
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct InclusionPath<H: Hasher, Arity: PoseidonArity> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    path: Vec<PathElement<H, Arity>>,
}
*/

type InclusionPath[H HasherDomain] struct {
	Path []PathElement[H] `json:"path"`
}

/*
#[derive(Debug, Default, Clone, Serialize, Deserialize)]
pub struct PathElement<H: Hasher, Arity: PoseidonArity> {
    #[serde(bound(
        serialize = "H::Domain: Serialize",
        deserialize = "H::Domain: Deserialize<'de>"
    ))]
    hashes: Vec<H::Domain>,
    index: usize,
    #[serde(skip)]
    _arity: PhantomData<Arity>,
}
*/

type PathElement[H HasherDomain] struct {
	Hashes []H    `json:"hashes"`
	Index  uint64 `json:"index"`
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
