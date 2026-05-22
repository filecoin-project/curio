package webrpc

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"

	cliutil "github.com/filecoin-project/lotus/cli/util"
)

type RpcInfo struct {
	Address   string
	CLayers   []string
	Reachable bool
	SyncState string
	Version   string
}

func (a *WebRPC) SyncerState(ctx context.Context) ([]RpcInfo, error) {
	type minimalApiInfo struct {
		Apis struct {
			ChainApiInfo []string
		}
	}

	var rpcInfos []string
	confNameToAddr := make(map[string][]string) // config name -> api addresses

	err := config.ForEachConfig[minimalApiInfo](ctx, a.deps.DB, func(name string, info minimalApiInfo) error {
		if len(info.Apis.ChainApiInfo) == 0 {
			return nil
		}

		for _, addr := range info.Apis.ChainApiInfo {
			rpcInfos = append(rpcInfos, addr)
			ai := cliutil.ParseApiInfo(addr)
			confNameToAddr[name] = append(confNameToAddr[name], ai.Addr)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	dedup := map[string]bool{} // for dedup by address

	infos := map[string]RpcInfo{} // api address -> rpc info
	var infosLk sync.Mutex

	var wg sync.WaitGroup
	for _, info := range rpcInfos {
		ai := cliutil.ParseApiInfo(info)
		if dedup[ai.Addr] {
			continue
		}
		dedup[ai.Addr] = true
		wg.Go(func() {
			var clayers []string
			for layer, adrs := range confNameToAddr {
				for _, adr := range adrs {
					if adr == ai.Addr {
						clayers = append(clayers, layer)
					}
				}
			}

			myinfo := RpcInfo{
				Address:   ai.Addr,
				Reachable: false,
				CLayers:   clayers,
			}
			defer func() {
				infosLk.Lock()
				defer infosLk.Unlock()
				infos[ai.Addr] = myinfo
			}()

			addr, err := ai.DialArgs("v1")
			if err != nil {
				log.Warnf("could not get DialArgs: %w", err)
			}

			var res api.ChainStruct
			closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
				api.GetInternalStructs(&res), ai.AuthHeader(), []jsonrpc.Option{jsonrpc.WithErrors(jsonrpc.NewErrors())}...)
			if err != nil {
				log.Warnf("error creating jsonrpc client: %v", err)
				return
			}
			defer closer()

			full := &res

			ver, err := full.Version(ctx)
			if err != nil {
				log.Warnw("Version", "error", err)
				return
			}

			head, err := full.ChainHead(ctx)
			if err != nil {
				log.Warnw("ChainHead", "error", err)
				return
			}

			var syncState string
			switch {
			case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*3/2): // within 1.5 epochs
				syncState = "ok"
			case time.Now().Unix()-int64(head.MinTimestamp()) < int64(build.BlockDelaySecs*5): // within 5 epochs
				syncState = fmt.Sprintf("slow (%s behind)", time.Since(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second))
			default:
				syncState = fmt.Sprintf("behind (%s behind)", time.Since(time.Unix(int64(head.MinTimestamp()), 0)).Truncate(time.Second))
			}

			myinfo = RpcInfo{
				Address:   ai.Addr,
				CLayers:   clayers,
				Reachable: true,
				Version:   ver.Version,
				SyncState: syncState,
			}
		})
	}
	wg.Wait()

	var infoList []RpcInfo
	for _, i := range infos {
		infoList = append(infoList, i)
	}
	sort.Slice(infoList, func(i, j int) bool {
		return infoList[i].Address < infoList[j].Address
	})

	return infoList, nil
}
