package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	ltypes "github.com/filecoin-project/curio/api/types"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/api"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

type Curio interface {
	// MethodGroup: Curio
	//The common method group contains administration methods

	Version(context.Context) ([]int, error)             //perm:admin
	Shutdown(context.Context) error                     //perm:admin
	Cordon(context.Context) error                       //perm:admin
	Uncordon(context.Context) error                     //perm:admin
	Info(ctx context.Context) (*ltypes.NodeInfo, error) //perm:read

	// MethodGroup: Deal
	//The deal method group contains method for adding deals to sector

	AllocatePieceToSector(ctx context.Context, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (api.SectorOffset, error) //perm:write

	// MethodGroup: Storage
	//The storage method group contains are storage administration method

	StorageInit(ctx context.Context, path string, opts storiface.LocalStorageMeta) error                                                                                   //perm:admin
	StorageAddLocal(ctx context.Context, path string) error                                                                                                                //perm:admin
	StorageDetachLocal(ctx context.Context, path string) error                                                                                                             //perm:admin
	StorageList(ctx context.Context) (map[storiface.ID][]storiface.Decl, error)                                                                                            //perm:admin
	StorageLocal(ctx context.Context) (map[storiface.ID]string, error)                                                                                                     //perm:admin
	StorageStat(ctx context.Context, id storiface.ID) (fsutil.FsStat, error)                                                                                               //perm:admin
	StorageInfo(context.Context, storiface.ID) (storiface.StorageInfo, error)                                                                                              //perm:admin
	StorageFindSector(ctx context.Context, sector abi.SectorID, ft storiface.SectorFileType, ssize abi.SectorSize, allowFetch bool) ([]storiface.SectorStorageInfo, error) //perm:admin
	StorageGenerateVanillaProof(ctx context.Context, maddr address.Address, sector abi.SectorNumber) ([]byte, error)                                                       //perm:admin

	// MethodGroup: Log
	//The log method group has logging methods

	LogList(ctx context.Context) ([]string, error)                  //perm:read
	LogSetLevel(ctx context.Context, subsystem, level string) error //perm:admin
}
