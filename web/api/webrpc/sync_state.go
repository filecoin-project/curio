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
	"github.com/filecoin-project/curio/deps"

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
	endpoints, err := deps.CollectChainRPCEndpoints(ctx, a.Deps.DB)
	if err != nil {
		return nil, err
	}

	dedup := map[string]bool{} // for dedup by address

	infos := map[string]RpcInfo{} // api address -> rpc info
	var infosLk sync.Mutex

	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		ai := cliutil.ParseApiInfo(endpoint.ApiInfo)
		if dedup[ai.Addr] {
			continue
		}
		dedup[ai.Addr] = true

		displayAddr := ai.Addr

		wg.Go(func() {
			clayers := append([]string(nil), endpoint.Layers...)
			myinfo := RpcInfo{
				Address:   displayAddr,
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
				return
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

			version := ver.Version

			myinfo = RpcInfo{
				Address:   displayAddr,
				CLayers:   clayers,
				Reachable: true,
				Version:   version,
				SyncState: syncState,
			}
		})
	}
	wg.Wait()

	infoList := make([]RpcInfo, 0, len(infos))
	for _, i := range infos {
		infoList = append(infoList, i)
	}
	sort.Slice(infoList, func(i, j int) bool {
		return infoList[i].Address < infoList[j].Address
	})

	return infoList, nil
}
