package webrpc

import (
	"context"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/store"
)

var log = logging.Logger("webrpc")

type WebRPC struct {
	deps      *deps.Deps
	taskSPIDs map[string]SpidGetter
	stor      adt.Store
}

func (a *WebRPC) Version(context.Context) (string, error) {
	return build.UserVersion(), nil
}

func (a *WebRPC) BlockDelaySecs(context.Context) (uint64, error) {
	return build.BlockDelaySecs, nil
}

func Routes(r *mux.Router, deps *deps.Deps) {
	handler := &WebRPC{
		deps:      deps,
		stor:      store.ActorStore(context.Background(), blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(deps.Chain), curiochain.ChainBlockCache)),
		taskSPIDs: makeTaskSPIDs(),
	}

	rpcSrv := jsonrpc.NewServer()
	rpcSrv.Register("CurioWeb", handler)
	r.Handle("/v0", rpcSrv)
}
