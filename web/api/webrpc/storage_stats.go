package webrpc

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

type StorageGCStats struct {
	Actor int64 `db:"sp_id"`
	Count int   `db:"count"`
	Miner string
}

func (a *WebRPC) StorageGCStats(ctx context.Context) ([]*StorageGCStats, error) {
	var stats []*StorageGCStats
	err := a.deps.DB.Select(ctx, &stats, `SELECT sp_id, count(*) as count FROM storage_removal_marks GROUP BY sp_id ORDER BY sp_id DESC`)
	if err != nil {
		return nil, err
	}

	for _, s := range stats {
		maddr, err := address.NewIDAddress(uint64(s.Actor))
		if err != nil {
			return nil, err
		}
		s.Miner = maddr.String()
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

	CanSeal  bool `db:"can_seal"`
	CanStore bool `db:"can_store"`

	Urls string `db:"urls"`

	// db ignored
	TypeName string `db:"-"`
	PathType string `db:"-"`

	Miner string
}

func (a *WebRPC) StorageGCMarks(ctx context.Context, limit int, offset int) ([]*StorageGCMarks, int, error) {
	var marks []*StorageGCMarks

	var total int

	// Get the total count of rows
	err := a.deps.DB.Select(ctx, &total, `SELECT COUNT(*) FROM storage_removal_marks`)
	if err != nil {
		return nil, 0, xerrors.Errorf("querying storage removal marks: %w", err)
	}

	err = a.deps.DB.Select(ctx, &marks, `
		SELECT m.sp_id, m.sector_num, m.sector_filetype, m.storage_id, m.created_at, m.approved, m.approved_at, sl.can_seal, sl.can_store, sl.urls
			FROM storage_removal_marks m LEFT JOIN storage_path sl ON m.storage_id = sl.storage_id
			ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	minerMap := make(map[int64]address.Address)

	for i, m := range marks {
		marks[i].TypeName = storiface.SectorFileType(m.FileType).String()

		var pathRole []string
		if m.CanSeal {
			pathRole = append(pathRole, "Scratch")
		}
		if m.CanStore {
			pathRole = append(pathRole, "Store")
		}
		marks[i].PathType = strings.Join(pathRole, "/")

		us := paths.UrlsFromString(m.Urls)
		us = lo.Map(us, func(u string, _ int) string {
			return must.One(url.Parse(u)).Host
		})
		marks[i].Urls = strings.Join(us, ", ")
		maddr, ok := minerMap[marks[i].Actor]
		if !ok {
			maddr, err := address.NewIDAddress(uint64(marks[i].Actor))
			if err != nil {
				return nil, 0, err
			}
			minerMap[marks[i].Actor] = maddr
		}
		marks[i].Miner = maddr.String()
	}

	return marks, total, nil
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
