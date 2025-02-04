package webrpc

import (
	"context"
	"net/url"
	"sort"
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

// StorageStoreStats holds the aggregated store (can_store) stats for a given file type.
type StorageStoreStats struct {
	Type      string `json:"type"`
	Capacity  int64  `json:"capacity"`
	Available int64  `json:"available"`
	Used      int64  `json:"used"`

	CapStr    string `json:"cap_str"`
	UseStr    string `json:"use_str"`
	AvailStr  string `json:"avail_str"`
}

// storagePathRow mirrors the columns we need from the storage_path table.
type storagePathRow struct {
	StorageID  string `db:"storage_id"`
	AllowTypes string `db:"allow_types"`
	DenyTypes  string `db:"deny_types"`
	Capacity   int64  `db:"capacity"`
	Available  int64  `db:"available"`
	CanStore   bool   `db:"can_store"`
}

// parseCSV splits a comma‐separated string into a slice of trimmed strings.
// An empty string returns an empty slice.
func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, part := range parts {
		t := strings.TrimSpace(part)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// StorageStoreTypeStats returns aggregated capacity and available space for store-type paths,
// grouped by each file type. A given storage path’s capacity is counted for a file type if its
// allow/deny lists (if set) permit that file type. In addition, if the resulting list contains
// more than a chosen threshold of entries, the lower-volume ones are merged into a single "Other" entry.
func (a *WebRPC) StorageStoreTypeStats(ctx context.Context) ([]StorageStoreStats, error) {
	// Query all storage paths that are available for storing.
	var paths []*storagePathRow
	const query = `
		SELECT storage_id, allow_types, deny_types, capacity, available, can_store
		FROM storage_path
		WHERE can_store = true
	`
	if err := a.deps.DB.Select(ctx, &paths, query); err != nil {
		return nil, xerrors.Errorf("querying storage paths: %w", err)
	}

	// map paths into by-storage-id
	byStorageID := make(map[string]*storagePathRow)
	for _, p := range paths {
		byStorageID[p.StorageID] = p
	}

	// map paths into by-type
	pathsForTypeMap := make(map[storiface.SectorFileType][]string)
	for _, p := range paths {
		allowList := parseCSV(p.AllowTypes)
		denyList := parseCSV(p.DenyTypes)
		for _, ft := range storiface.PathTypes {
			if ft.Allowed(allowList, denyList) {
				pathsForTypeMap[ft] = append(pathsForTypeMap[ft], p.StorageID)
			}
		}
	}

	// figure out "Other" which is the most shared set of storage ids
	sharedMap := make(map[string]int)
	for _, paths := range pathsForTypeMap {
		pathSetStr := strings.Join(paths, ",")
		sharedMap[pathSetStr]++
	}

	// find the most shared set of storage ids
	var mostSharedPathSet string
	var mostSharedCount int
	for pathSetStr, count := range sharedMap {
		if count > mostSharedCount {
			mostSharedCount = count
			mostSharedPathSet = pathSetStr
		}
	}

	// remove the most shared path set from the map
	for ft, paths := range pathsForTypeMap {
		if strings.Join(paths, ",") == mostSharedPathSet {
			delete(pathsForTypeMap, ft)
		}
	}

	// add the "Other" entry
	ftOther := storiface.FTNone
	pathsForTypeMap[ftOther] = strings.Split(mostSharedPathSet, ",")

	// Aggregate per file type based on the computed pathsForTypeMap,
	// then if there is more than one entry, compute overall storage totals and return
	// a top-level "Storage" entry with sub-entries (their Type field is prefixed to indicate sub-level).
	var statsByType []StorageStoreStats
	for ft, storageIDs := range pathsForTypeMap {
		var capacity, available int64
		seen := make(map[string]bool)
		for _, sid := range storageIDs {
			if seen[sid] {
				continue
			}
			seen[sid] = true
			if sp, ok := byStorageID[sid]; ok {
				capacity += sp.Capacity
				available += sp.Available
			}
		}
		used := capacity - available
		var label string
		if ft == storiface.FTNone {
			label = "other"
		} else {
			label = ft.String()
		}
		statsByType = append(statsByType, StorageStoreStats{
			Type:      label,
			Capacity:  capacity,
			Available: available,
			Used:      used,
			CapStr:    types.SizeStr(types.NewInt(uint64(capacity))),
			UseStr:    types.SizeStr(types.NewInt(uint64(used))),
			AvailStr:  types.SizeStr(types.NewInt(uint64(available))),
		})
	}

	// Sort the file-type breakdown descending by Capacity.
	sort.Slice(statsByType, func(i, j int) bool {
		return statsByType[i].Capacity > statsByType[j].Capacity
	})

	// If there is more than one entry, create a top-level "Storage" row that sums overall capacity/available,
	// and then show the breakdown as sub-entries (by indenting the Type field).
	if len(statsByType) == 1 {
		return []StorageStoreStats{}, nil
	}

	sort.Slice(statsByType, func(i, j int) bool {
		// Compute available percentage (Available / Capacity) for each entry.
		// If Capacity is zero, use 0 to avoid division by zero.
		var percI, percJ float64
		if statsByType[i].Capacity > 0 {
			percI = float64(statsByType[i].Available) / float64(statsByType[i].Capacity)
		}
		if statsByType[j].Capacity > 0 {
			percJ = float64(statsByType[j].Available) / float64(statsByType[j].Capacity)
		}
		return percI < percJ
	})

	// Otherwise, if there is only one aggregated entry, return that.
	return statsByType, nil
}

type StorageGCMark struct {
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

type StorageGCMarks struct {
	Marks []*StorageGCMark
	Total int
}

func (a *WebRPC) StorageGCMarks(ctx context.Context, miner *string, sectorNum *int64, limit int, offset int) (*StorageGCMarks, error) {
	var spID *int64
	if miner != nil {
		maddr, err := address.NewFromString(*miner)
		if err != nil {
			return nil, err
		}
		sp_id, err := address.IDFromAddress(maddr)
		if err != nil {
			return nil, err
		}
		tspid := int64(sp_id)
		spID = &tspid
	}

	if sectorNum != nil && *sectorNum < 0 {
		return nil, xerrors.Errorf("invalid sector_num: %d", *sectorNum)
	}

	var marks []*StorageGCMark
	var total int

	// Get the total count of rows
	err := a.deps.DB.QueryRow(ctx, `SELECT 
    											COUNT(*) 
											FROM storage_removal_marks 
											WHERE 
											     ($1::BIGINT IS NULL OR sp_id = $1) 
											  AND ($2::BIGINT IS NULL OR sector_num = $2)`, spID, sectorNum).Scan(&total)
	if err != nil {
		return nil, xerrors.Errorf("querying storage removal marks: %w", err)
	}

	err = a.deps.DB.Select(ctx, &marks, `
								SELECT 
								    m.sp_id, 
								    m.sector_num, 
								    m.sector_filetype, 
								    m.storage_id, 
								    m.created_at, 
								    m.approved, 
								    m.approved_at, 
								    sl.can_seal, 
								    sl.can_store, 
								    sl.urls
								FROM storage_removal_marks m 
								    LEFT JOIN storage_path sl ON m.storage_id = sl.storage_id
								WHERE 
								    ($1::BIGINT IS NULL OR m.sp_id = $1)
    								AND ($2::BIGINT IS NULL OR m.sector_num = $2) 
								ORDER BY created_at 
								DESC LIMIT $3 
								OFFSET $4`, spID, sectorNum, limit, offset)
	if err != nil {
		return nil, xerrors.Errorf("querying storage removal marks: %w", err)
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
			maddr, err = address.NewIDAddress(uint64(marks[i].Actor))
			if err != nil {
				return nil, err
			}
			minerMap[marks[i].Actor] = maddr
		}
		marks[i].Miner = maddr.String()
	}

	return &StorageGCMarks{
		Marks: marks,
		Total: total,
	}, nil
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
