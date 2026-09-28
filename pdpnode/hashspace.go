package pdpnode

import (
	"context"
	"net/http"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/paths"
)

// NewHashSpace joins this node's long-term storage paths into the cluster
// hash spaces. A path that denies piece park is still joined: first setup
// and new disks are not given ranges there, and any pieces already on it
// are vacated. Nodes with no long-term paths still get a cluster handle so
// they can read and delete pieces held elsewhere. Returns nil on a
// read-only database.
func NewHashSpace(ctx context.Context, db *harmonydb.DB, local *paths.Local, si paths.SectorIndex, auth http.Header) (*hashspace.Cluster, error) {
	if db.ReadOnly() {
		return nil, nil
	}
	sps, err := local.Local(ctx)
	if err != nil {
		return nil, xerrors.Errorf("listing local storage: %w", err)
	}
	var drives []hashspace.LocalDrive
	for _, sp := range sps {
		if !sp.CanStore {
			continue
		}
		if _, err := si.StorageInfo(ctx, sp.ID); err != nil {
			return nil, xerrors.Errorf("storage info %s: %w", sp.ID, err)
		}
		drives = append(drives, hashspace.LocalDrive{StorageID: string(sp.ID), Root: sp.LocalPath})
	}
	return hashspace.NewCluster(ctx, db, drives, auth)
}
