package lmrpc

import (
	"net/http"
	_ "net/http/pprof"

	"github.com/gorilla/mux"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/rpcenc"
	"github.com/filecoin-project/lotus/metrics/proxy"
	"github.com/filecoin-project/lotus/node/impl"
)

// MinerHandler returns a miner handler, to be mounted as-is on the server.
func MinerHandler(a api.StorageMiner, permissioned bool) (http.Handler, error) {
	mapi := proxy.MetricedStorMinerAPI(a)
	if permissioned {
		mapi = api.PermissionedStorMinerAPI(mapi)
	}

	readerHandler, readerServerOpt := rpcenc.ReaderParamDecoder()
	rpcServer := jsonrpc.NewServer(jsonrpc.WithServerErrors(api.RPCErrors), readerServerOpt)
	rpcServer.Register("Filecoin", mapi)
	rpcServer.AliasMethod("rpc.discover", "Filecoin.Discover")

	rootMux := mux.NewRouter()

	// remote storage
	if _, realImpl := a.(*impl.StorageMinerAPI); realImpl {
		m := mux.NewRouter()
		m.PathPrefix("/remote").HandlerFunc(a.(*impl.StorageMinerAPI).ServeRemote(permissioned))

		var hnd http.Handler = m
		if permissioned {
			hnd = &auth.Handler{
				Verify: a.StorageAuthVerify,
				Next:   m.ServeHTTP,
			}
		}

		rootMux.PathPrefix("/remote").Handler(hnd)
	}

	// local APIs
	{
		m := mux.NewRouter()
		m.Handle("/rpc/v0", rpcServer)
		m.Handle("/rpc/streams/v0/push/{uuid}", readerHandler)

		var hnd http.Handler = m
		if permissioned {
			hnd = &auth.Handler{
				Verify: a.AuthVerify,
				Next:   m.ServeHTTP,
			}
		}

		rootMux.PathPrefix("/").Handler(hnd)
	}

	return rootMux, nil
}
