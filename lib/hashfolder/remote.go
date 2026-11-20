package hashfolder

import (
	"context"
	"net/http"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// RemoteClient deletes pieces through their storage owners without joining
// local disks or starting background maintenance. It needs no shutdown handling.
type RemoteClient struct {
	db     *harmonydb.DB
	auth   http.Header
	client *http.Client
}

func NewRemoteClient(db *harmonydb.DB, auth http.Header) *RemoteClient {
	return &RemoteClient{db: db, auth: auth, client: &http.Client{}}
}

// DeleteCID uses the same routing and deletion logic as a storage-owning
// cluster. Each call reads current routing without retaining a stale snapshot.
func (c *RemoteClient) DeleteCID(ctx context.Context, pieceCID string) error {
	cluster := &Cluster{db: c.db, getRPCAuth: func() (http.Header, error) {
		return c.auth, nil
	}, client: c.client}
	cluster.Public = cluster.newSpace(DIR_OPEN)
	cluster.ACL = cluster.newSpace(DIR_ACL)
	return cluster.DeleteCID(ctx, pieceCID)
}
