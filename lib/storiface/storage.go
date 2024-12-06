package storiface

import (
	"context"
	"io"
	"net/http"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"
)

type Data = io.Reader

// Reader is a fully-featured Reader. It is the
// union of the standard IO sequential access method (Read), with seeking
// ability (Seek), as well random access (ReadAt).
type Reader interface {
	io.Closer
	io.Reader
	io.ReaderAt
	io.Seeker
}

type SectorRef struct {
	ID        abi.SectorID
	ProofType abi.RegisteredSealProof
}

// PieceNumber is a reference to a piece in the storage system; mapping between
// pieces in the storage system and piece CIDs is maintained by the storage index
type PieceNumber uint64

func (pn PieceNumber) Ref() SectorRef {
	return SectorRef{
		ID:        abi.SectorID{Miner: 0, Number: abi.SectorNumber(pn)},
		ProofType: abi.RegisteredSealProof_StackedDrg64GiBV1, // This only cares about TreeD which is the same for all sizes
	}
}

type PreCommit1Out []byte

type SectorCids struct {
	Unsealed cid.Cid
	Sealed   cid.Cid
}

type ReplicaUpdateProof []byte

type Verifier interface {
	VerifySeal(proof.SealVerifyInfo) (bool, error)
	VerifyAggregateSeals(aggregate proof.AggregateSealVerifyProofAndInfos) (bool, error)
	VerifyReplicaUpdate(update proof.ReplicaUpdateInfo) (bool, error)
	VerifyWinningPoSt(ctx context.Context, info proof.WinningPoStVerifyInfo) (bool, error)
	VerifyWindowPoSt(ctx context.Context, info proof.WindowPoStVerifyInfo) (bool, error)

	GenerateWinningPoStSectorChallenge(context.Context, abi.RegisteredPoStProof, abi.ActorID, abi.PoStRandomness, uint64) ([]uint64, error)
}

// Prover contains cheap proving-related methods
type Prover interface {
	// TODO: move GenerateWinningPoStSectorChallenge from the Verifier interface to here

	AggregateSealProofs(aggregateInfo proof.AggregateSealVerifyProofAndInfos, proofs [][]byte) ([]byte, error)
}

type SectorLocation struct {
	// Local when set to true indicates to lotus that sector data is already
	// available locally; When set lotus will skip fetching sector data, and
	// only check that sector data exists in sector storage
	Local bool

	// URL to the sector data
	// For sealed/unsealed sector, lotus expects octet-stream
	// For cache, lotus expects a tar archive with cache files
	// Valid schemas:
	// - http:// / https://
	URL string

	// optional http headers to use when requesting sector data
	Headers []SecDataHttpHeader
}

func (sd *SectorLocation) HttpHeaders() http.Header {
	out := http.Header{}
	for _, header := range sd.Headers {
		out[header.Key] = append(out[header.Key], header.Value)
	}
	return out
}

// note: we can't use http.Header as that's backed by a go map, which is all kinds of messy

type SecDataHttpHeader struct {
	Key   string
	Value string
}

// StorageConfig .lotusstorage/storage.json
type StorageConfig struct {
	StoragePaths []LocalPath
}

type LocalPath struct {
	Path string
}

// LocalStorageMeta [path]/sectorstore.json
type LocalStorageMeta struct {
	ID ID

	// A high weight means data is more likely to be stored in this path
	Weight uint64 // 0 = readonly

	// Intermediate data for the sealing process will be stored here
	CanSeal bool

	// Finalized sectors that will be proved over time will be stored here
	CanStore bool

	// MaxStorage specifies the maximum number of bytes to use for sector storage
	// (0 = unlimited)
	MaxStorage uint64

	// List of storage groups this path belongs to
	Groups []string

	// List of storage groups to which data from this path can be moved. If none
	// are specified, allow to all
	AllowTo []string

	// AllowTypes lists sector file types which are allowed to be put into this
	// path. If empty, all file types are allowed.
	//
	// Valid values:
	// - "unsealed"
	// - "sealed"
	// - "cache"
	// - "update"
	// - "update-cache"
	// Any other value will generate a warning and be ignored.
	AllowTypes []string

	// DenyTypes lists sector file types which aren't allowed to be put into this
	// path.
	//
	// Valid values:
	// - "unsealed"
	// - "sealed"
	// - "cache"
	// - "update"
	// - "update-cache"
	// Any other value will generate a warning and be ignored.
	DenyTypes []string

	// AllowMiners lists miner IDs which are allowed to store their sector data into
	// this path. If empty, all miner IDs are allowed
	AllowMiners []string

	// DenyMiners lists miner IDs which are denied to store their sector data into
	// this path
	DenyMiners []string
}
