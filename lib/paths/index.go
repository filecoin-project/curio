package paths

import (
	"context"
	storiface2 "github.com/filecoin-project/curio/lib/storiface"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

var HeartbeatInterval = 10 * time.Second
var SkippedHeartbeatThresh = HeartbeatInterval * 5

//go:generate go run github.com/golang/mock/mockgen -destination=mocks/index.go -package=mocks . SectorIndex

type SpaceUseFunc func(ft storiface2.SectorFileType, ssize abi.SectorSize) (uint64, error)

type spaceUseCtxKey struct{}

var SpaceUseKey = spaceUseCtxKey{}

type SectorIndex interface { // part of storage-miner api
	StorageAttach(context.Context, storiface2.StorageInfo, fsutil.FsStat) error
	StorageDetach(ctx context.Context, id storiface2.ID, url string) error
	StorageInfo(context.Context, storiface2.ID) (storiface2.StorageInfo, error)
	StorageReportHealth(context.Context, storiface2.ID, storiface2.HealthReport) error

	StorageDeclareSector(ctx context.Context, storageID storiface2.ID, s abi.SectorID, ft storiface2.SectorFileType, primary bool) error
	StorageDropSector(ctx context.Context, storageID storiface2.ID, s abi.SectorID, ft storiface2.SectorFileType) error
	StorageFindSector(ctx context.Context, sector abi.SectorID, ft storiface2.SectorFileType, ssize abi.SectorSize, allowFetch bool) ([]storiface2.SectorStorageInfo, error)
	BatchStorageDeclareSectors(ctx context.Context, declarations []SectorDeclaration) error

	StorageBestAlloc(ctx context.Context, allocate storiface2.SectorFileType, ssize abi.SectorSize, pathType storiface2.PathType, miner abi.ActorID) ([]storiface2.StorageInfo, error)

	// atomically acquire locks on all sector file types. close ctx to unlock
	StorageLock(ctx context.Context, sector abi.SectorID, read storiface2.SectorFileType, write storiface2.SectorFileType) error
	StorageTryLock(ctx context.Context, sector abi.SectorID, read storiface2.SectorFileType, write storiface2.SectorFileType) (bool, error)
	StorageGetLocks(ctx context.Context) (storiface2.SectorLocks, error)

	StorageList(ctx context.Context) (map[storiface2.ID][]storiface2.Decl, error)
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
