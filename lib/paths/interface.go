package paths

import (
	"context"
	"io"
	"net/url"
	"os"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/partialfile"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

//go:generate go run github.com/golang/mock/mockgen -source=interface.go -destination=mocks/pf.go -package=mocks PartialFileHandler

// PartialFileHandler helps mock out the partial file functionality during testing.
type PartialFileHandler interface {
	// OpenPartialFile opens and returns a partial file at the given path and also verifies it has the given
	// size
	OpenPartialFile(maxPieceSize abi.PaddedPieceSize, path string) (*partialfile.PartialFile, error)

	// HasAllocated returns true if the given partial file has an unsealed piece starting at the given offset with the given size.
	// returns false otherwise.
	HasAllocated(pf *partialfile.PartialFile, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error)

	// Reader returns a file from which we can read the unsealed piece in the partial file.
	Reader(pf *partialfile.PartialFile, offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (io.Reader, error)

	// Close closes the partial file
	Close(pf *partialfile.PartialFile) error
}

//go:generate go run github.com/golang/mock/mockgen -source=interface.go -destination=mocks/store.go -package=mocks Store

type Store interface {
	AcquireSector(ctx context.Context, s storiface.SectorRef, existing storiface.SectorFileType, allocate storiface.SectorFileType, sealing storiface.PathType, op storiface.AcquireMode, opts ...storiface.AcquireOption) (paths storiface.SectorPaths, stores storiface.SectorPaths, err error)
	Remove(ctx context.Context, s abi.SectorID, types storiface.SectorFileType, force bool, keepIn []storiface.ID) error

	// like remove, but doesn't remove the primary sector copy, nor the last
	// non-primary copy if there no primary copies
	RemoveCopies(ctx context.Context, s abi.SectorID, types storiface.SectorFileType) error

	// move sectors into storage
	MoveStorage(ctx context.Context, s storiface.SectorRef, types storiface.SectorFileType, opts ...storiface.AcquireOption) error

	FsStat(ctx context.Context, id storiface.ID) (fsutil.FsStat, error)

	Reserve(ctx context.Context, sid storiface.SectorRef, ft storiface.SectorFileType, storageIDs storiface.SectorPaths, overheadTab map[storiface.SectorFileType]int, minFreePercentage float64) (func(), error)

	GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error)
	GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error)
	ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error)
}

// StashStore provides methods for managing stashes within storage paths.
// Stashes are temporary files located in the "stash/" subdirectory of sealing paths.
// They are removed on startup and are not indexed. Stashes are used to store
// arbitrary data and can be served or removed as needed.
type StashStore interface {
	// StashCreate creates a new stash file with the specified maximum size.
	// It selects a sealing path with the most available space and creates a file
	// named [uuid].tmp in the stash directory.
	//
	// The provided writeFunc is called with an *os.File pointing to the newly
	// created stash file, allowing the caller to write data into it.
	//
	// Parameters:
	//  - ctx: Context for cancellation and timeout.
	//  - maxSize: The maximum size of the stash file in bytes.
	//  - writeFunc: A function that writes data to the stash file.
	//
	// Returns:
	//  - uuid.UUID: A unique identifier for the created stash.
	//  - error: An error if the stash could not be created.
	StashCreate(ctx context.Context, maxSize int64, writeFunc func(f *os.File) error) (uuid.UUID, error)

	// ServeAndRemove serves the stash file identified by the given UUID as an io.ReadCloser.
	// Once the stash has been fully read (i.e., the last byte has been read),
	// the stash file is automatically removed.
	// If the read is incomplete (e.g., due to an error or premature closure),
	// the stash file remains on disk.
	//
	// Parameters:
	//  - ctx: Context for cancellation and timeout.
	//  - id: The UUID of the stash to serve.
	//
	// Returns:
	//  - io.ReadCloser: A reader for the stash file.
	//  - error: An error if the stash could not be served.
	ServeAndRemove(ctx context.Context, id uuid.UUID) (io.ReadCloser, error)

	// StashRemove removes the stash file identified by the given UUID.
	//
	// Parameters:
	//  - ctx: Context for cancellation and timeout.
	//  - id: The UUID of the stash to remove.
	//
	// Returns:
	//  - error: An error if the stash could not be removed.
	StashRemove(ctx context.Context, id uuid.UUID) error

	// StashURL generates a URL for accessing the stash identified by the given UUID.
	//
	// Parameters:
	//  - id: The UUID of the stash.
	//
	// Returns:
	//  - url.URL: The URL where the stash can be accessed.
	//  - error: An error if the URL could not be generated.
	StashURL(id uuid.UUID) (url.URL, error)
}
