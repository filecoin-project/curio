package piecestore

import (
	"context"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("piecestore")

// Store implements PieceIO using local/remote path storage without filecoin-ffi.
type Store struct {
	storage    *paths.Remote
	localStore *paths.Local
	sindex     paths.SectorIndex
}

var _ PieceIO = (*Store)(nil)

func New(st *paths.Remote, ls *paths.Local, si paths.SectorIndex) *Store {
	return &Store{
		storage:    st,
		localStore: ls,
		sindex:     si,
	}
}

func (s *Store) acquireSector(ctx context.Context, taskID *harmonytask.TaskID, sector storiface.SectorRef, existing, allocate storiface.SectorFileType, sealing storiface.PathType) (storiface.SectorPaths, storiface.SectorPaths, func(...storiface.SectorFileType), error) {
	sectorPaths, storageIDs, err := s.storage.AcquireSector(ctx, sector, existing, allocate, sealing, storiface.AcquireMove)
	if err != nil {
		return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, err
	}

	releaseStorage, err := s.localStore.Reserve(ctx, sector, allocate, storageIDs, storiface.FSOverheadSeal, paths.MinFreeStoragePercentage)
	if err != nil {
		return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("reserving storage space: %w", err)
	}

	return sectorPaths, storageIDs, func(dontDeclare ...storiface.SectorFileType) {
		releaseStorage()

	nextType:
		for _, fileType := range storiface.PathTypes {
			if fileType&allocate == 0 {
				continue
			}
			for _, dont := range dontDeclare {
				if fileType&dont != 0 {
					continue nextType
				}
			}

			sid := storiface.PathByType(storageIDs, fileType)
			if err := s.sindex.StorageDeclareSector(ctx, storiface.ID(sid), sector.ID, fileType, true); err != nil {
				log.Errorf("declare sector error: %+v", err)
			}
		}
	}, nil
}

func (s *Store) ensureOneCopy(ctx context.Context, sid abi.SectorID, pathIDs storiface.SectorPaths, fts storiface.SectorFileType) error {
	if !pathIDs.HasAllSet(fts) {
		return xerrors.Errorf("ensure one copy: not all paths are set")
	}

	for _, fileType := range fts.AllSet() {
		pid := storiface.PathByType(pathIDs, fileType)
		keepIn := []storiface.ID{storiface.ID(pid)}

		if err := s.storage.Remove(ctx, sid, fileType, true, keepIn); err != nil {
			return err
		}
	}

	return nil
}
