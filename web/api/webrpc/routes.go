package webrpc

import (
	"context"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	lbuild "github.com/filecoin-project/lotus/build"
)

var log = logging.Logger("webrpc")

type WebRPC struct {
	deps      *deps.Deps
	taskSPIDs map[string]SpidGetter
}

func (a *WebRPC) Version(context.Context) (string, error) {
	return build.UserVersion(), nil
}

func (a *WebRPC) BlockDelaySecs(context.Context) (uint64, error) {
	return lbuild.BlockDelaySecs, nil
}

func Routes(r *mux.Router, deps *deps.Deps, activeTasks []harmonytask.TaskInterface) {
	handler := &WebRPC{
		deps:      deps,
		taskSPIDs: makeTaskSPIDs(activeTasks),
	}

	rpcSrv := jsonrpc.NewServer()
	rpcSrv.Register("CurioWeb", handler)
	r.Handle("/v0", rpcSrv)
}
