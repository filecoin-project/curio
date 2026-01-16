package webrpc

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

// StoragePathInfo contains detailed information about a storage path
type StoragePathInfo struct {
	StorageID     string     `db:"storage_id"`
	URLs          *string    `db:"urls"`
	Weight        *int64     `db:"weight"`
	MaxStorage    *int64     `db:"max_storage"`
	CanSeal       *bool      `db:"can_seal"`
	CanStore      *bool      `db:"can_store"`
	Groups        *string    `db:"groups"`
	AllowTo       *string    `db:"allow_to"`
	AllowTypes    *string    `db:"allow_types"`
	DenyTypes     *string    `db:"deny_types"`
	Capacity      *int64     `db:"capacity"`
	Available     *int64     `db:"available"`
	FSAvailable   *int64     `db:"fs_available"`
	Reserved      *int64     `db:"reserved"`
	Used          *int64     `db:"used"`
	LastHeartbeat *time.Time `db:"last_heartbeat"`
	HeartbeatErr  *string    `db:"heartbeat_err"`
	AllowMiners   string     `db:"allow_miners"`
	DenyMiners    string     `db:"deny_miners"`

	// Computed fields for UI (not from DB)
	PathType        string   `json:"PathType,omitempty"`
	CapacityStr     string   `json:"CapacityStr,omitempty"`
	AvailableStr    string   `json:"AvailableStr,omitempty"`
	FSAvailableStr  string   `json:"FSAvailableStr,omitempty"`
	ReservedStr     string   `json:"ReservedStr,omitempty"`
	UsedStr         string   `json:"UsedStr,omitempty"`
	MaxStorageStr   string   `json:"MaxStorageStr,omitempty"`
	UsedPercent     float64  `json:"UsedPercent,omitempty"`
	ReservedPercent float64  `json:"ReservedPercent,omitempty"`
	HealthStatus    string   `json:"HealthStatus,omitempty"`
	HealthOK        bool     `json:"HealthOK,omitempty"`
	URLList         []string `json:"URLList,omitempty"`
	HostList        []string `json:"HostList,omitempty"`
	GroupList       []string `json:"GroupList,omitempty"`
	AllowToList     []string `json:"AllowToList,omitempty"`
	AllowTypesList  []string `json:"AllowTypesList,omitempty"`
	DenyTypesList   []string `json:"DenyTypesList,omitempty"`
	AllowMinersList []string `json:"AllowMinersList,omitempty"`
	DenyMinersList  []string `json:"DenyMinersList,omitempty"`
}

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
	MinerID   int64     `db:"sp_id" json:"MinerID"`
	SectorNum int64     `db:"sector_num" json:"SectorNum"`
	FileType  int64     `db:"sector_filetype" json:"FileType"`
	CreatedAt time.Time `db:"created_at" json:"CreatedAt"`
	Approved  bool      `db:"approved" json:"Approved"`

	// Computed
	Miner        string `json:"Miner"`
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
	TotalSectorEntries int                       `json:"TotalSectorEntries"`
	PrimaryEntries     int                       `json:"PrimaryEntries"`
	SecondaryEntries   int                       `json:"SecondaryEntries"`
	ByType             []StoragePathTypeSummary  `json:"ByType"`
	ByMiner            []StoragePathMinerSummary `json:"ByMiner"`
	PendingGC          int                       `json:"PendingGC"`
	ApprovedGC         int                       `json:"ApprovedGC"`
}

func (a *WebRPC) StoragePathList(ctx context.Context) ([]*StoragePathInfo, error) {
	var pathsList []*StoragePathInfo
	err := a.deps.DB.Select(ctx, &pathsList, `SELECT 
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
		deny_miners FROM storage_path ORDER BY storage_id`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query storage paths: %w", err)
	}

	for _, p := range pathsList {
		enrichStoragePathInfo(p)
	}

	return pathsList, nil
}

func (a *WebRPC) StoragePathDetail(ctx context.Context, storageID string) (*StoragePathDetailResult, error) {
	// Get basic path info
	path := StoragePathInfo{}
	err := a.deps.DB.QueryRow(ctx, `SELECT 
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

	enrichStoragePathInfo(&path)

	result := &StoragePathDetailResult{
		Info: &path,
	}

	// Get URL liveness
	err = a.deps.DB.Select(ctx, &result.URLs, `
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
		u.LastCheckedStr = formatTimeAgo(u.LastChecked)
		if u.LastLive != nil {
			u.LastLiveStr = formatTimeAgo(*u.LastLive)
		}
		if u.LastDead != nil {
			u.LastDeadStr = formatTimeAgo(*u.LastDead)
		}
		if parsed, err := url.Parse(u.URL); err == nil {
			u.Host = parsed.Host
		} else {
			u.Host = u.URL
		}
	}

	// Get sector summary by type
	var typeSummary []struct {
		FileType int64 `db:"sector_filetype"`
		Count    int   `db:"count"`
		Primary  int   `db:"primary_count"`
	}
	err = a.deps.DB.Select(ctx, &typeSummary, `
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

	// Get sector summary by miner
	var minerSummary []struct {
		MinerID int64 `db:"miner_id"`
		Count   int   `db:"count"`
		Primary int   `db:"primary_count"`
	}
	err = a.deps.DB.Select(ctx, &minerSummary, `
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

	// Get GC marks
	err = a.deps.DB.Select(ctx, &result.GCMarks, `
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
		m.CreatedAtStr = formatTimeAgo(m.CreatedAt)
		if m.Approved {
			result.ApprovedGC++
		} else {
			result.PendingGC++
		}
	}

	return result, nil
}

// StoragePathSectors returns paginated sectors for a storage path
func (a *WebRPC) StoragePathSectors(ctx context.Context, storageID string, limit, offset int) (*StoragePathSectorsResult, error) {
	// Get total count
	var total int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM sector_location WHERE storage_id = $1`, storageID).Scan(&total)
	if err != nil {
		return nil, xerrors.Errorf("counting sectors: %w", err)
	}

	var sectors []StoragePathSector
	err = a.deps.DB.Select(ctx, &sectors, `
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

// Helper functions

func enrichStoragePathInfo(info *StoragePathInfo) {
	// Path type
	canSeal := info.CanSeal != nil && *info.CanSeal
	canStore := info.CanStore != nil && *info.CanStore
	switch {
	case canSeal && canStore:
		info.PathType = "Seal + Store"
	case canSeal:
		info.PathType = "Seal"
	case canStore:
		info.PathType = "Store"
	default:
		info.PathType = "Read-Only"
	}

	// Format sizes
	if info.Capacity != nil {
		info.CapacityStr = types.SizeStr(types.NewInt(uint64(*info.Capacity)))
	}
	if info.Available != nil {
		info.AvailableStr = types.SizeStr(types.NewInt(uint64(*info.Available)))
	}
	if info.FSAvailable != nil {
		info.FSAvailableStr = types.SizeStr(types.NewInt(uint64(*info.FSAvailable)))
	}
	if info.Reserved != nil {
		info.ReservedStr = types.SizeStr(types.NewInt(uint64(*info.Reserved)))
	}
	if info.Capacity != nil && info.FSAvailable != nil {
		used := *info.Capacity - *info.FSAvailable
		info.UsedStr = types.SizeStr(types.NewInt(uint64(used)))
	}
	if info.MaxStorage != nil && *info.MaxStorage > 0 {
		info.MaxStorageStr = types.SizeStr(types.NewInt(uint64(*info.MaxStorage)))
	}

	// Percentages
	if info.Capacity != nil && *info.Capacity > 0 {
		if info.FSAvailable != nil {
			info.UsedPercent = float64(*info.Capacity-*info.FSAvailable) * 100 / float64(*info.Capacity)
		}
		if info.Reserved != nil {
			info.ReservedPercent = float64(*info.Reserved) * 100 / float64(*info.Capacity)
		}
	}

	// Health status
	if info.LastHeartbeat != nil {
		heartbeatAge := time.Since(*info.LastHeartbeat)
		if info.HeartbeatErr != nil {
			info.HealthStatus = "Error: " + *info.HeartbeatErr
			info.HealthOK = false
		} else if heartbeatAge > paths.SkippedHeartbeatThresh {
			info.HealthStatus = "Stale (" + heartbeatAge.Round(time.Second).String() + " ago)"
			info.HealthOK = false
		} else {
			info.HealthStatus = "OK (" + heartbeatAge.Round(time.Second).String() + " ago)"
			info.HealthOK = true
		}
	} else {
		info.HealthStatus = "Unknown"
		info.HealthOK = false
	}

	// Parse URL lists
	if info.URLs != nil {
		info.URLList = paths.UrlsFromString(*info.URLs)
		info.HostList = make([]string, 0, len(info.URLList))
		for _, u := range info.URLList {
			if parsed, err := url.Parse(u); err == nil {
				info.HostList = append(info.HostList, parsed.Host)
			}
		}
	}

	// Parse other lists
	if info.Groups != nil {
		info.GroupList = splitTrimmed(*info.Groups)
	}
	if info.AllowTo != nil {
		info.AllowToList = splitTrimmed(*info.AllowTo)
	}
	if info.AllowTypes != nil {
		info.AllowTypesList = splitTrimmed(*info.AllowTypes)
	}
	if info.DenyTypes != nil {
		info.DenyTypesList = splitTrimmed(*info.DenyTypes)
	}
	info.AllowMinersList = splitTrimmed(info.AllowMiners)
	info.DenyMinersList = splitTrimmed(info.DenyMiners)
}

func splitTrimmed(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	} else if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("2006-01-02 15:04")
}

// StoragePathsSummary returns summary info about all storage paths sorted by capacity
func (a *WebRPC) StoragePathsSummary(ctx context.Context) ([]*StoragePathInfo, error) {
	pathsInfo, err := a.StoragePathList(ctx)
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

// HostToMachineID returns a map of host:port to machine ID for linking purposes
func (a *WebRPC) HostToMachineID(ctx context.Context, hosts []string) (map[string]int64, error) {
	if len(hosts) == 0 {
		return map[string]int64{}, nil
	}

	var machines []struct {
		ID          int64  `db:"id"`
		HostAndPort string `db:"host_and_port"`
	}
	err := a.deps.DB.Select(ctx, &machines, `SELECT id, host_and_port FROM harmony_machines`)
	if err != nil {
		return nil, xerrors.Errorf("querying machines: %w", err)
	}

	result := make(map[string]int64)
	for _, m := range machines {
		// Extract just the host:port part for matching
		for _, h := range hosts {
			// Match if the machine's host_and_port contains the host or equals it
			if m.HostAndPort == h || strings.Contains(m.HostAndPort, h) || strings.Contains(h, m.HostAndPort) {
				result[h] = m.ID
				break
			}
			// Also try matching just the hostname part (before colon) with machine's host
			hostOnly := strings.Split(h, ":")[0]
			machineHost := strings.Split(m.HostAndPort, ":")[0]
			if hostOnly == machineHost {
				result[h] = m.ID
			}
		}
	}

	return result, nil
}
