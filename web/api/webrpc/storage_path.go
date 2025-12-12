package webrpc

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

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
}

func (a *WebRPC) StoragePathList(ctx context.Context) ([]*StoragePathInfo, error) {
	var paths []*StoragePathInfo
	err := a.deps.DB.Select(ctx, &paths, `SELECT 
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
		deny_miners FROM storage_path`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query storage paths: %w", err)
	}
	return paths, nil
}

func (a *WebRPC) StoragePathDetail(ctx context.Context, storageID string) (*StoragePathInfo, error) {
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
	return &path, nil
}
