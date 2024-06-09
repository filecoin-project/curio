package webrpc

import (
	"context"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"time"
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

	// Ignored
	Type   string `db:"-"`
	UseStr string `db:"-"`
	CapStr string `db:"-"`
}

func (a *WebRPC) StorageUseStats(ctx context.Context) ([]StorageUseStats, error) {
	var stats []StorageUseStats

	err := a.deps.DB.Select(ctx, &stats, `SELECT can_seal, can_store, SUM(available) as available, SUM(capacity) as capacity FROM storage_path GROUP BY can_seal, can_store`)
	if err != nil {
		return nil, err
	}

	for i, st := range stats {
		switch {
		case st.CanSeal && st.CanStore:
			stats[i].Type = "Seal/Store"
		case st.CanSeal:
			stats[i].Type = "Seal"
		case st.CanStore:
			stats[i].Type = "Store"
		default:
			stats[i].Type = "None"
		}

		stats[i].UseStr = types.SizeStr(types.NewInt(uint64(stats[i].Capacity - stats[i].Available)))
		stats[i].CapStr = types.SizeStr(types.NewInt(uint64(stats[i].Capacity)))
	}

	return stats, nil
}

type StorageGCMarks struct {
	Actor     int64  `db:"sp_id"`
	SectorNum int64  `db:"sector_num"`
	FileType  int64  `db:"sector_filetype"`
	StorageID string `db:"storage_id"`

	CreatedAt  time.Time  `db:"created_at"`
	Approved   bool       `db:"approved"`
	ApprovedAt *time.Time `db:"approved_at"`

	// db ignored
	TypeName string `db:"-"`
}

func (a *WebRPC) StorageGCMarks(ctx context.Context) ([]StorageGCMarks, error) {
	var marks []StorageGCMarks
	err := a.deps.DB.Select(ctx, &marks, `SELECT sp_id, sector_num, sector_filetype, storage_id, created_at, approved, approved_at FROM storage_removal_marks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}

	for i, m := range marks {
		marks[i].TypeName = storiface.SectorFileType(m.FileType).String()
	}

	return marks, nil
}

func (a *WebRPC) StorageGCApprove(ctx context.Context, actor int64, sectorNum int64, fileType int64, storageID string) error {
	now := time.Now()
	_, err := a.deps.DB.Exec(ctx, `UPDATE storage_removal_marks SET approved = true, approved_at = $1 WHERE sp_id = $2 AND sector_num = $3 AND sector_filetype = $4 AND storage_id = $5`, now, actor, sectorNum, fileType, storageID)
	if err != nil {
		return err
	}
	return nil
}

func (a *WebRPC) StorageGCApproveAll(ctx context.Context) error {
	now := time.Now()
	_, err := a.deps.DB.Exec(ctx, `UPDATE storage_removal_marks SET approved = true, approved_at = $1 WHERE approved = false`, now)
	if err != nil {
		return err
	}
	return nil
}

func (a *WebRPC) StorageGCUnapproveAll(ctx context.Context) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE storage_removal_marks SET approved = false, approved_at = NULL WHERE approved = true`)
	if err != nil {
		return err
	}
	return nil
}
