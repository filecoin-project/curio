package proof

import "encoding/json"

var testMarshal = false

type HasherDomain = any

type Sha256Domain [32]byte

func (s Sha256Domain) MarshalJSON() ([]byte, error) {
	if testMarshal {
		return json.Marshal(s[:])
	}

	return json.Marshal([32]byte(s))
}

type PoseidonDomain [32]byte // Fr

func (p PoseidonDomain) MarshalJSON() ([]byte, error) {
	if testMarshal {
		return json.Marshal(p[:])
	}

	return json.Marshal([32]byte(p))
}

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
