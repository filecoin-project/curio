package webrpcporep

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

// StoragePathInfo is the shared storage mount info type.
type StoragePathInfo = webrpc.StoragePathInfo

// StoragePathURLLiveness contains URL health information
type StoragePathURLLiveness struct {
	URL            string     `db:"url" json:"URL"`
	LastChecked    time.Time  `db:"last_checked" json:"LastChecked"`
	LastLive       *time.Time `db:"last_live" json:"LastLive"`
	LastDead       *time.Time `db:"last_dead" json:"LastDead"`
	LastDeadReason *string    `db:"last_dead_reason" json:"LastDeadReason"`

	// Computed
	IsLive         bool   `json:"IsLive"`
	Host           string `json:"Host"`
	LastCheckedStr string `json:"LastCheckedStr"`
	LastLiveStr    string `json:"LastLiveStr"`
	LastDeadStr    string `json:"LastDeadStr"`
}

// StoragePathSector contains information about a sector stored on this path
type StoragePathSector struct {
	MinerID      int64 `db:"miner_id" json:"MinerID"`
	SectorNum    int64 `db:"sector_num" json:"SectorNum"`
	FileType     int64 `db:"sector_filetype" json:"FileType"`
	IsPrimary    bool  `db:"is_primary" json:"IsPrimary"`
	ReadRefs     int   `db:"read_refs" json:"ReadRefs"`
	HasWriteLock bool  `db:"has_write_lock" json:"HasWriteLock"`

	// Computed
	Miner       string `json:"Miner"`
	FileTypeStr string `json:"FileTypeStr"`
}

// StoragePathGCMark contains GC marks for this storage path
type StoragePathGCMark struct {
	MinerID    int64     `db:"sp_id" json:"MinerID"`
	SectorNum int64     `db:"sector_num" json:"SectorNum"`
	FileType  int64     `db:"sector_filetype" json:"FileType"`
	CreatedAt time.Time `db:"created_at" json:"CreatedAt"`
	Approved  bool      `db:"approved" json:"Approved"`

	// Computed
	Miner         string `json:"Miner"`
	FileTypeStr  string `json:"FileTypeStr"`
	CreatedAtStr string `json:"CreatedAtStr"`
}

// StoragePathTypeSummary provides counts by file type
type StoragePathTypeSummary struct {
	FileType string `json:"FileType"`
	Count    int    `json:"Count"`
	Primary  int    `json:"Primary"`
}

// StoragePathMinerSummary provides counts by miner
type StoragePathMinerSummary struct {
	Miner   string `json:"Miner"`
	Count   int    `json:"Count"`
	Primary int    `json:"Primary"`
}

// StoragePathSectorsResult wraps paginated sector results
type StoragePathSectorsResult struct {
	Sectors []StoragePathSector `json:"Sectors"`
	Total   int                 `json:"Total"`
}

// StoragePathDetailResult contains all details about a storage path
type StoragePathDetailResult struct {
	Info    *StoragePathInfo         `json:"Info"`
	URLs    []StoragePathURLLiveness `json:"URLs"`
	GCMarks []StoragePathGCMark      `json:"GCMarks"`

	// Summary stats
	TotalSectorEntries int                      `json:"TotalSectorEntries"`
	PrimaryEntries     int                      `json:"PrimaryEntries"`
	SecondaryEntries   int                      `json:"SecondaryEntries"`
	ByType             []StoragePathTypeSummary `json:"ByType"`
	ByMiner            []StoragePathMinerSummary `json:"ByMiner"`
	PendingGC          int                      `json:"PendingGC"`
	ApprovedGC         int                      `json:"ApprovedGC"`
}

// StoragePathsSummary returns summary info about all storage paths sorted by capacity
func (a *PoRep) StoragePathsSummary(ctx context.Context) ([]*StoragePathInfo, error) {
	pathsInfo, err := a.Handler.StoragePathList(ctx)
	if err != nil {
		return nil, err
	}

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

func (a *PoRep) StoragePathDetail(ctx context.Context, storageID string) (*StoragePathDetailResult, error) {
	path := StoragePathInfo{}
	err := a.Deps.DB.QueryRow(ctx, `SELECT 
		storage_id, 
		urls, 
		weight, 
		max_storage, 
		can_seal, 
		can_store, 
		groups, 
		allow_to, 
		allow_types, 
		deny_types, 
		capacity, 
		available, 
		fs_available, 
		reserved, 
		used, 
		last_heartbeat, 
		heartbeat_err, 
		allow_miners, 
		deny_miners FROM storage_path WHERE storage_id = $1`, storageID).
		Scan(
			&path.StorageID,
			&path.URLs,
			&path.Weight,
			&path.MaxStorage,
			&path.CanSeal,
			&path.CanStore,
			&path.Groups,
			&path.AllowTo,
			&path.AllowTypes,
			&path.DenyTypes,
			&path.Capacity,
			&path.Available,
			&path.FSAvailable,
			&path.Reserved,
			&path.Used,
			&path.LastHeartbeat,
			&path.HeartbeatErr,
			&path.AllowMiners,
			&path.DenyMiners)
	if err != nil {
		return nil, xerrors.Errorf("failed to query storage path detail: %w", err)
	}

	webrpc.EnrichStoragePathInfo(&path)
	if a.Deps.LocalStore != nil {
		if locals, lerr := a.Deps.LocalStore.Local(ctx); lerr == nil {
			for _, lp := range locals {
				if string(lp.ID) == path.StorageID && lp.LocalPath != "" {
					path.LocalPath = lp.LocalPath
					break
				}
			}
		}
	}

	result := &StoragePathDetailResult{
		Info: &path,
	}

	err = a.Deps.DB.Select(ctx, &result.URLs, `
		SELECT url, last_checked, last_live, last_dead, last_dead_reason
		FROM sector_path_url_liveness
		WHERE storage_id = $1
		ORDER BY url
	`, storageID)
	if err != nil {
		return nil, xerrors.Errorf("querying URL liveness: %w", err)
	}

	for i := range result.URLs {
		u := &result.URLs[i]
		u.IsLive = u.LastLive != nil && (u.LastDead == nil || u.LastLive.After(*u.LastDead))
		u.LastCheckedStr = webrpc.FormatTimeAgo(u.LastChecked)
		if u.LastLive != nil {
			u.LastLiveStr = webrpc.FormatTimeAgo(*u.LastLive)
		}
		if u.LastDead != nil {
			u.LastDeadStr = webrpc.FormatTimeAgo(*u.LastDead)
		}
		if parsed, err := url.Parse(u.URL); err == nil {
			u.Host = parsed.Host
		} else {
			u.Host = u.URL
		}
	}

	var typeSummary []struct {
		FileType int64 `db:"sector_filetype"`
		Count    int   `db:"count"`
		Primary  int   `db:"primary_count"`
	}
	err = a.Deps.DB.Select(ctx, &typeSummary, `
		SELECT sector_filetype, COUNT(*) as count, SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) as primary_count
		FROM sector_location
		WHERE storage_id = $1
		GROUP BY sector_filetype
		ORDER BY count DESC
	`, storageID)
	if err != nil {
		return nil, xerrors.Errorf("querying sector type summary: %w", err)
	}

	for _, ts := range typeSummary {
		result.ByType = append(result.ByType, StoragePathTypeSummary{
			FileType: storiface.SectorFileType(ts.FileType).String(),
			Count:    ts.Count,
			Primary:  ts.Primary,
		})
		result.TotalSectorEntries += ts.Count
		result.PrimaryEntries += ts.Primary
	}
	result.SecondaryEntries = result.TotalSectorEntries - result.PrimaryEntries

	var minerSummary []struct {
		MinerID int64 `db:"miner_id"`
		Count   int   `db:"count"`
		Primary int   `db:"primary_count"`
	}
	err = a.Deps.DB.Select(ctx, &minerSummary, `
		SELECT miner_id, COUNT(*) as count, SUM(CASE WHEN is_primary THEN 1 ELSE 0 END) as primary_count
		FROM sector_location
		WHERE storage_id = $1
		GROUP BY miner_id
		ORDER BY count DESC
	`, storageID)
	if err != nil {
		return nil, xerrors.Errorf("querying sector miner summary: %w", err)
	}

	for _, ms := range minerSummary {
		miner := fmt.Sprintf("f0%d", ms.MinerID)
		if maddr, err := address.NewIDAddress(uint64(ms.MinerID)); err == nil {
			miner = maddr.String()
		}
		result.ByMiner = append(result.ByMiner, StoragePathMinerSummary{
			Miner:   miner,
			Count:   ms.Count,
			Primary: ms.Primary,
		})
	}

	err = a.Deps.DB.Select(ctx, &result.GCMarks, `
		SELECT sp_id, sector_num, sector_filetype, created_at, approved
		FROM storage_removal_marks
		WHERE storage_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`, storageID)
	if err != nil {
		return nil, xerrors.Errorf("querying GC marks: %w", err)
	}

	for i := range result.GCMarks {
		m := &result.GCMarks[i]
		if maddr, err := address.NewIDAddress(uint64(m.MinerID)); err == nil {
			m.Miner = maddr.String()
		} else {
			m.Miner = fmt.Sprintf("f0%d", m.MinerID)
		}
		m.FileTypeStr = storiface.SectorFileType(m.FileType).String()
		m.CreatedAtStr = webrpc.FormatTimeAgo(m.CreatedAt)
		if m.Approved {
			result.ApprovedGC++
		} else {
			result.PendingGC++
		}
	}

	return result, nil
}

// StoragePathSectors returns paginated sectors for a storage path
func (a *PoRep) StoragePathSectors(ctx context.Context, storageID string, limit, offset int) (*StoragePathSectorsResult, error) {
	var total int
	err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM sector_location WHERE storage_id = $1`, storageID).Scan(&total)
	if err != nil {
		return nil, xerrors.Errorf("counting sectors: %w", err)
	}

	var sectors []StoragePathSector
	err = a.Deps.DB.Select(ctx, &sectors, `
		SELECT miner_id, sector_num, sector_filetype, is_primary, read_refs,
			(write_ts IS NOT NULL) as has_write_lock 
		FROM sector_location 
		WHERE storage_id = $1
		ORDER BY miner_id, sector_num, sector_filetype 
		LIMIT $2 OFFSET $3`, storageID, limit, offset)
	if err != nil {
		return nil, xerrors.Errorf("querying sectors: %w", err)
	}

	for i := range sectors {
		s := &sectors[i]
		if maddr, err := address.NewIDAddress(uint64(s.MinerID)); err == nil {
			s.Miner = maddr.String()
		} else {
			s.Miner = fmt.Sprintf("f0%d", s.MinerID)
		}
		s.FileTypeStr = storiface.SectorFileType(s.FileType).String()
	}

	return &StoragePathSectorsResult{
		Sectors: sectors,
		Total:   total,
	}, nil
}

// HostToMachineID returns a map of host:port to machine ID for linking purposes
func (a *PoRep) HostToMachineID(ctx context.Context, hosts []string) (map[string]int64, error) {
	if len(hosts) == 0 {
		return map[string]int64{}, nil
	}

	var machines []struct {
		ID          int64  `db:"id"`
		HostAndPort string `db:"host_and_port"`
	}
	err := a.Deps.DB.Select(ctx, &machines, `SELECT id, host_and_port FROM harmony_machines`)
	if err != nil {
		return nil, xerrors.Errorf("querying machines: %w", err)
	}

	result := make(map[string]int64)
	for _, h := range hosts {
		if _, ok := result[h]; ok {
			continue
		}
		for _, m := range machines {
			if m.HostAndPort == h || strings.Contains(m.HostAndPort, h) || strings.Contains(h, m.HostAndPort) {
				result[h] = m.ID
				break
			}
			hostOnly := strings.Split(h, ":")[0]
			machineHost := strings.Split(m.HostAndPort, ":")[0]
			if hostOnly == machineHost {
				result[h] = m.ID
				break
			}
		}
	}

	return result, nil
}
