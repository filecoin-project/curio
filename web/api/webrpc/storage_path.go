package webrpc

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/chain/types"
)

// StoragePathInfo contains detailed information about a storage path / mount.
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
	LocalPath       string   `json:"LocalPath,omitempty"`
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

// StoragePathList returns all storage mounts with capacity / health stats.
func (a *Handler) StoragePathList(ctx context.Context) ([]*StoragePathInfo, error) {
	var pathsList []*StoragePathInfo
	err := a.Deps.DB.Select(ctx, &pathsList, `SELECT 
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

	localByID := map[string]string{}
	if a.Deps.LocalStore != nil {
		if locals, lerr := a.Deps.LocalStore.Local(ctx); lerr == nil {
			for _, lp := range locals {
				if lp.LocalPath != "" {
					localByID[string(lp.ID)] = lp.LocalPath
				}
			}
		}
	}

	for _, p := range pathsList {
		if local, ok := localByID[p.StorageID]; ok {
			p.LocalPath = local
		}
		EnrichStoragePathInfo(p)
	}

	return pathsList, nil
}

// EnrichStoragePathInfo fills computed UI fields on a storage path row.
func EnrichStoragePathInfo(info *StoragePathInfo) {
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

	if info.Capacity != nil && *info.Capacity > 0 {
		if info.FSAvailable != nil {
			info.UsedPercent = float64(*info.Capacity-*info.FSAvailable) * 100 / float64(*info.Capacity)
		}
		if info.Reserved != nil {
			info.ReservedPercent = float64(*info.Reserved) * 100 / float64(*info.Capacity)
		}
	}

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

	if info.URLs != nil {
		info.URLList = paths.UrlsFromString(*info.URLs)
		info.HostList = make([]string, 0, len(info.URLList))
		for _, u := range info.URLList {
			if parsed, err := url.Parse(u); err == nil {
				info.HostList = append(info.HostList, parsed.Host)
			}
		}
	}

	if info.Groups != nil {
		info.GroupList = SplitTrimmed(*info.Groups)
	}
	if info.AllowTo != nil {
		info.AllowToList = SplitTrimmed(*info.AllowTo)
	}
	if info.AllowTypes != nil {
		info.AllowTypesList = SplitTrimmed(*info.AllowTypes)
	}
	if info.DenyTypes != nil {
		info.DenyTypesList = SplitTrimmed(*info.DenyTypes)
	}
	info.AllowMinersList = SplitTrimmed(info.AllowMiners)
	info.DenyMinersList = SplitTrimmed(info.DenyMiners)
}

// SplitTrimmed splits a comma-separated string and trims whitespace.
func SplitTrimmed(s string) []string {
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

// FormatTimeAgo returns a short relative time string.
func FormatTimeAgo(t time.Time) string {
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
