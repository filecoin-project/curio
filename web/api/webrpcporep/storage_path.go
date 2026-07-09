package webrpcporep

import (
	"context"
	"sort"

	"github.com/filecoin-project/curio/web/api/webrpc"
)

// StoragePathInfo is the shared storage mount info type.
type StoragePathInfo = webrpc.StoragePathInfo
type StoragePathDetailResult = webrpc.StoragePathDetailResult
type StoragePathSectorsResult = webrpc.StoragePathSectorsResult

// StoragePathsSummary returns summary info about all storage paths sorted by capacity
func (a *PoRep) StoragePathsSummary(ctx context.Context) ([]*StoragePathInfo, error) {
	pathsInfo, err := a.Handler.StoragePathList(ctx)
	if err != nil {
		return nil, err
	}

	// Sort by capacity descending
	sort.Slice(pathsInfo, func(i, j int) bool {
		capI := int64(0)
		capJ := int64(0)
		if pathsInfo[i].Capacity != nil {
			capI = *pathsInfo[i].Capacity
		}
		if pathsInfo[j].Capacity != nil {
			capJ = *pathsInfo[j].Capacity
		}
		return capI > capJ
	})

	return pathsInfo, nil
}
