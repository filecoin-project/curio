package paths

import (
	"context"
	storiface2 "github.com/filecoin-project/curio/lib/storiface"
	"io"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
	"github.com/filecoin-project/lotus/storage/sealer/partialfile"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

//go:generate go run github.com/golang/mock/mockgen -destination=mocks/pf.go -package=mocks . PartialFileHandler

// PartialFileHandler helps mock out the partial file functionality during testing.
type PartialFileHandler interface {
	// OpenPartialFile opens and returns a partial file at the given path and also verifies it has the given
	// size
	OpenPartialFile(maxPieceSize abi.PaddedPieceSize, path string) (*partialfile.PartialFile, error)

	// HasAllocated returns true if the given partial file has an unsealed piece starting at the given offset with the given size.
	// returns false otherwise.
	HasAllocated(pf *partialfile.PartialFile, offset storiface2.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error)

	// Reader returns a file from which we can read the unsealed piece in the partial file.
	Reader(pf *partialfile.PartialFile, offset storiface2.PaddedByteIndex, size abi.PaddedPieceSize) (io.Reader, error)

	// Close closes the partial file
	Close(pf *partialfile.PartialFile) error
}

//go:generate go run github.com/golang/mock/mockgen -destination=mocks/store.go -package=mocks . Store

type Store interface {
	AcquireSector(ctx context.Context, s storiface2.SectorRef, existing storiface2.SectorFileType, allocate storiface2.SectorFileType, sealing storiface2.PathType, op storiface2.AcquireMode, opts ...storiface2.AcquireOption) (paths storiface2.SectorPaths, stores storiface2.SectorPaths, err error)
	Remove(ctx context.Context, s abi.SectorID, types storiface2.SectorFileType, force bool, keepIn []storiface2.ID) error

	// like remove, but doesn't remove the primary sector copy, nor the last
	// non-primary copy if there no primary copies
	RemoveCopies(ctx context.Context, s abi.SectorID, types storiface2.SectorFileType) error

	// move sectors into storage
	MoveStorage(ctx context.Context, s storiface2.SectorRef, types storiface2.SectorFileType, opts ...storiface2.AcquireOption) error

	FsStat(ctx context.Context, id storiface2.ID) (fsutil.FsStat, error)

	Reserve(ctx context.Context, sid storiface2.SectorRef, ft storiface2.SectorFileType, storageIDs storiface2.SectorPaths, overheadTab map[storiface2.SectorFileType]int, minFreePercentage float64) (func(), error)

	GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error)
	GeneratePoRepVanillaProof(ctx context.Context, sr storiface2.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error)
	ReadSnapVanillaProof(ctx context.Context, sr storiface2.SectorRef) ([]byte, error)
}
