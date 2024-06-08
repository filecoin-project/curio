package client

import (
	"context"
	"net/http"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/api"

	"github.com/filecoin-project/lotus/api/v1api"
)

// NewCurioRpc creates a new http jsonrpc client.
func NewCurioRpc(ctx context.Context, addr string, requestHeader http.Header) (api.Curio, jsonrpc.ClientCloser, error) {
	var res v1api.CurioStruct

	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		api.GetInternalStructs(&res), requestHeader, jsonrpc.WithErrors(RPCErrors))

	return &res, closer, err
}

var RPCErrors = jsonrpc.NewErrors()
