package webrpc

import "context"

type StorageGCStats struct {
	Actor int64 `db:"sp_id"`
	Count int   `db:"count"`
}

func (a *WebRPC) StorageGCStats(ctx context.Context) ([]StorageGCStats, error) {
	var stats []StorageGCStats
	err := a.deps.DB.Select(ctx, &stats, `SELECT sp_id, count(*) as count FROM storage_removal_marks GROUP BY sp_id ORDER BY sp_id DESC`)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

type StorageUseStats struct {
	UsedSeal, TotalSeal   int64
	UsedStore, TotalStore int64
}

func (a *WebRPC) StorageUseStats(ctx context.Context) (StorageUseStats, error) {
	var stats []StorageUseStats

	err := a.deps.DB.Select(ctx, &stats, `SELECT `)

}
