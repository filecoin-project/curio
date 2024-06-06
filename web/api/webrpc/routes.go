package webrpc

import (
	"context"

	"github.com/filecoin-project/curio/deps"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/build"
	lbuild "github.com/filecoin-project/lotus/build"
)

var log = logging.Logger("webrpc")

type WebRPC struct {
	deps *deps.Deps
}

func (a *WebRPC) Version(context.Context) (string, error) {
	return build.UserVersion(), nil
}

func (a *WebRPC) BlockDelaySecs(context.Context) (uint64, error) {
	return lbuild.BlockDelaySecs, nil
}

func Routes(r *mux.Router, deps *deps.Deps) {
	handler := &WebRPC{
		deps: deps,
	}

	rpcSrv := jsonrpc.NewServer()
	rpcSrv.Register("CurioWeb", handler)
	r.Handle("/v0", rpcSrv)
}
