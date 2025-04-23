package proof

import "github.com/filecoin-project/go-state-types/abi"

// FallbackPoStSectorProof corresponds to Rust:
//
//	#[derive(Clone, Debug, Serialize, Deserialize)]
//	pub struct FallbackPoStSectorProof<Tree: MerkleTreeTrait> {
//	    pub sector_id: SectorId,
//	    pub comm_r: <Tree::Hasher as Hasher>::Domain,
//	    pub vanilla_proof: Proof<<Tree as MerkleTreeTrait>::Proof>,
//	}
type FallbackPoStSectorProof struct {
	SectorID     uint64         `json:"sector_id"`
	CommR        PoseidonDomain `json:"comm_r"`
	VanillaProof VanillaProof   `json:"vanilla_proof"`
}

// VanillaProof parallels Rust’s
//
//	pub struct Proof<P: MerkleProofTrait> {
//	    pub sectors: Vec<SectorProof<P>>
//	}
type VanillaProof struct {
	// Each “SectorProof” holds merkle inclusion proofs, comm_c, comm_r_last, etc.
	Sectors []SectorProof `json:"sectors"`
}

// SectorProof parallels Rust’s
//
//	#[derive(Debug, Clone, Serialize, Deserialize)]
//	pub struct SectorProof<Proof: MerkleProofTrait> {
//	    pub inclusion_proofs: Vec<MerkleProof<...>>,
//	    pub comm_c: <Proof::Hasher as Hasher>::Domain,
//	    pub comm_r_last: <Proof::Hasher as Hasher>::Domain,
//	}
type SectorProof struct {
	// One or more “MerkleProof” objects describing the inclusion paths
	InclusionProofs []MerkleProof[PoseidonDomain] `json:"inclusion_proofs"`

	CommC     PoseidonDomain `json:"comm_c"`
	CommRLast PoseidonDomain `json:"comm_r_last"`
}

type PoStType int

const (
	PoStTypeWindow PoStType = iota
	PoStTypeWinning
)

/*
// These numbers must match those used for Window PoSt scheduling in the miner actor.
// Please coordinate changes with actor code.
// https://github.com/filecoin-project/specs-actors/blob/master/actors/abi/sector.go
pub static ref WINDOW_POST_SECTOR_COUNT: RwLock<HashMap<u64, usize>> = RwLock::new(

	[
	    (SECTOR_SIZE_2_KIB, 2),
	    (SECTOR_SIZE_4_KIB, 2),
	    (SECTOR_SIZE_16_KIB, 2),
	    (SECTOR_SIZE_32_KIB, 2),
	    (SECTOR_SIZE_8_MIB, 2),
	    (SECTOR_SIZE_16_MIB, 2),
	    (SECTOR_SIZE_512_MIB, 2),
	    (SECTOR_SIZE_1_GIB, 2),
	    (SECTOR_SIZE_32_GIB, 2349), // this gives 125,279,217 constraints, fitting in a single partition
	    (SECTOR_SIZE_64_GIB, 2300), // this gives 129,887,900 constraints, fitting in a single partition
	]
	.iter()
	.copied()
	.collect()

);
*/
var windowPostSectorCount = map[abi.SectorSize]int{
	2 << 10:   2,
	4 << 10:   2,
	16 << 10:  2,
	32 << 10:  2,
	8 << 20:   2,
	16 << 20:  2,
	512 << 20: 2,
	1 << 30:   2,
	32 << 30:  2349,
	64 << 30:  2300,
}

/*
pub const WINNING_POST_CHALLENGE_COUNT: usize = 66;
pub const WINNING_POST_SECTOR_COUNT: usize = 1;

pub const WINDOW_POST_CHALLENGE_COUNT: usize = 10;
*/

const (
	WinningPostChallengeCount = 66
	WinningPostSectorCount    = 1
	WindowPostChallengeCount  = 10
)

// PoStConfig: controlling how many sectors per partition, how many challenges, etc.
type PoStConfig struct {
	PoStType       PoStType
	SectorSize     abi.SectorSize
	SectorCount    int
	ChallengeCount int
	Priority       bool
}

/*
	fn window_post_info(sector_size: u64, api_version: ApiVersion) -> CircuitInfo {
	    with_shape!(
	        sector_size,
	        get_window_post_info,
	        &PoStConfig {
	            sector_size: SectorSize(sector_size),
	            challenge_count: WINDOW_POST_CHALLENGE_COUNT,
	            sector_count: *WINDOW_POST_SECTOR_COUNT
	                .read()
	                .expect("WINDOW_POST_SECTOR_COUNT poisoned")
	                .get(&sector_size)
	                .expect("unknown sector size"),
	            typ: PoStType::Window,
	            priority: true,
	            api_version,
	        }
	    )
	}
*/
func GetPoStConfig(sectorSize abi.SectorSize) *PoStConfig {
	return &PoStConfig{
		PoStType:       PoStTypeWindow,
		SectorSize:     sectorSize,
		SectorCount:    windowPostSectorCount[sectorSize],
		ChallengeCount: WindowPostChallengeCount,
		Priority:       true,
	}
}

type PublicReplicaInfo struct {
	CommR PoseidonDomain
}
