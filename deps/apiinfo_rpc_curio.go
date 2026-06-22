//go:build !maxboom

package deps

import (
	"github.com/filecoin-project/go-jsonrpc"

	lapi "github.com/filecoin-project/lotus/api"
)

func chainRPCErrorOpts() []jsonrpc.Option {
	return []jsonrpc.Option{jsonrpc.WithErrors(lapi.RPCErrors)}
}
