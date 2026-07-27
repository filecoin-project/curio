package webrpc

import (
	"context"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/store"
)

// Handler holds shared WebRPC state used by Skiff and Curio handlers.
type Handler struct {
	Deps      *deps.Deps
	TaskSPIDs map[string]SpidGetter
	Stor      adt.Store
}

// WebRPC is the JSON-RPC receiver for handlers shared by Skiff and Curio.
type WebRPC = Handler

func NewHandler(deps *deps.Deps) *Handler {
	return &Handler{
		Deps:      deps,
		Stor:      store.ActorStore(context.Background(), blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(deps.Chain), curiochain.ChainBlockCache)),
		TaskSPIDs: makeTaskSPIDs(),
	}
}
