//go:build skiff

package deps

import "github.com/filecoin-project/go-jsonrpc"

func chainRPCErrorOpts() []jsonrpc.Option {
	return []jsonrpc.Option{jsonrpc.WithErrors(RPCErrors)}
}
