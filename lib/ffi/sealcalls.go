package ffi

import (
	"context"

	logging "github.com/ipfs/go-log/v2"
	"github.com/puzpuzpuz/xsync/v2"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("cu/ffi")

type SealCalls struct {
	Pieces  *piecestore.Store
	Sectors *storageProvider
}

func NewSealCalls(st *paths.Remote, ls *paths.Local, si paths.SectorIndex) *SealCalls {
	return &SealCalls{
		Pieces: piecestore.New(st, ls, si),
		Sectors: &storageProvider{
			storage:             st,
			localStore:          ls,
			sindex:              si,
			storageReservations: xsync.NewIntegerMapOf[harmonytask.TaskID, []*StorageReservation](),
		},
	}
}

type storageProvider struct {
	storage             *paths.Remote
	localStore          *paths.Local
	sindex              paths.SectorIndex
	storageReservations *xsync.MapOf[harmonytask.TaskID, []*StorageReservation]
}

func (sb *SealCalls) LocalStorage(ctx context.Context) ([]storiface.StoragePath, error) {
	return sb.Sectors.localStore.Local(ctx)
}
