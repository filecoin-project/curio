package webrpc

import (
	"context"
	"reflect"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
)

var log = logging.Logger("webrpc")

func (a *Handler) Version(context.Context) (string, error) {
	return build.UserVersion(), nil
}

func (a *Handler) BlockDelaySecs(context.Context) (uint64, error) {
	return build.BlockDelaySecs, nil
}

func Routes(r *mux.Router, deps *deps.Deps, debug bool) {
	MountRPC(r, NewHandler(deps), debug)
}

func MountRPC(r *mux.Router, handler any, debug bool) {
	opt := []jsonrpc.ServerOption{}
	if debug {
		opt = append(opt, jsonrpc.WithTracer(func(method string, params []reflect.Value, results []reflect.Value, err error) {
			resNil := len(results) == 0 || (len(results) == 1 && results[0].IsNil())
			log.Infow("WebRPC call", "method", method, "params", params, "resultsNil?", resNil, "error", err)
		}))

	}
	rpcSrv := jsonrpc.NewServer(opt...)
	rpcSrv.Register("CurioWeb", handler)
	r.Handle("/v0", injectHTTPRequest(rpcSrv))
}
