package webrpc

import (
	"context"
	"golang.org/x/xerrors"
)

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
	CanSeal   bool `db:"can_seal"`
	CanStore  bool `db:"can_store"`
	Available int  `db:"available"`
	Capacity  int  `db:"capacity"`
}

func (a *WebRPC) StorageUseStats(ctx context.Context) (StorageUseStats, error) {
	var stats []StorageUseStats

	err := a.deps.DB.Select(ctx, &stats, `SELECT can_seal, can_store, SUM(available), SUM(capacity) FROM storage_path GROUP BY can_seal, can_store`)
	if err != nil {
		return StorageUseStats{}, err
	}
	if len(stats) == 0 {
		return StorageUseStats{}, xerrors.New("no storage stats")
	}
}
