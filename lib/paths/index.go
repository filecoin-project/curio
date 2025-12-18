package paths

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

var HeartbeatInterval = 10 * time.Second
var SkippedHeartbeatThresh = HeartbeatInterval * 5

//go:generate go run github.com/golang/mock/mockgen -destination=mocks/index.go -package=mocks . SectorIndex

type SpaceUseFunc func(ft storiface.SectorFileType, ssize abi.SectorSize) (uint64, error)

type spaceUseCtxKey struct{}

var SpaceUseKey = spaceUseCtxKey{}

type findSectorCacheKey struct{}

var FindSectorCacheKey = findSectorCacheKey{}

type SectorIndex interface { // part of storage-miner api
	StorageAttach(context.Context, storiface.StorageInfo, fsutil.FsStat) error
	StorageDetach(ctx context.Context, id storiface.ID, url string) error
	StorageInfo(context.Context, storiface.ID) (storiface.StorageInfo, error)
	StorageReportHealth(context.Context, storiface.ID, storiface.HealthReport) error

	StorageDeclareSector(ctx context.Context, storageID storiface.ID, s abi.SectorID, ft storiface.SectorFileType, primary bool) error
	StorageDropSector(ctx context.Context, storageID storiface.ID, s abi.SectorID, ft storiface.SectorFileType) error
	BatchStorageDeclareSectors(ctx context.Context, declarations []SectorDeclaration) error

	// FindSector can be cached if the ctx propagates a ttlcache instance over FindSectorCacheKey
	StorageFindSector(ctx context.Context, sector abi.SectorID, ft storiface.SectorFileType, ssize abi.SectorSize, allowFetch bool) ([]storiface.SectorStorageInfo, error)

	StorageBestAlloc(ctx context.Context, allocate storiface.SectorFileType, ssize abi.SectorSize, pathType storiface.PathType, miner abi.ActorID) ([]storiface.StorageInfo, error)

	// atomically acquire locks on all sector file types. close ctx to unlock
	StorageLock(ctx context.Context, sector abi.SectorID, read storiface.SectorFileType, write storiface.SectorFileType) error
	StorageTryLock(ctx context.Context, sector abi.SectorID, read storiface.SectorFileType, write storiface.SectorFileType) (bool, error)
	StorageGetLocks(ctx context.Context) (storiface.SectorLocks, error)

	StorageList(ctx context.Context) (map[storiface.ID][]storiface.Decl, error)
}

func MinerFilter(allowMiners, denyMiners []string, miner abi.ActorID) (bool, string, error) {
	checkMinerInList := func(minersList []string, miner abi.ActorID) (bool, error) {
		for _, m := range minersList {
			minerIDStr := m
			maddr, err := address.NewFromString(minerIDStr)
			if err != nil {
				return false, xerrors.Errorf("parsing miner address: %w", err)
			}
			mid, err := address.IDFromAddress(maddr)
			if err != nil {
				return false, xerrors.Errorf("converting miner address to ID: %w", err)
			}
			if abi.ActorID(mid) == miner {
				return true, nil
			}
		}
		return false, nil
	}

	if len(allowMiners) > 0 {
		found, err := checkMinerInList(allowMiners, miner)
		if err != nil || !found {
			return false, "not allowed", err
		}
	}

	if len(denyMiners) > 0 {
		found, err := checkMinerInList(denyMiners, miner)
		if err != nil || found {
			return false, "denied", err
		}
	}
	return true, "", nil
}
