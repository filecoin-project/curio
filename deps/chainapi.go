package deps

import (
	"context"
	"os"
	"sort"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// ChainRPCEndpoint is a chain JSON-RPC target for dashboards and monitoring.
type ChainRPCEndpoint struct {
	ApiInfo string
	Layers  []string
}

// CollectChainRPCEndpoints returns chain RPC targets from env and config.
func CollectChainRPCEndpoints(ctx context.Context, db *harmonydb.DB) ([]ChainRPCEndpoint, error) {
	if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
		return []ChainRPCEndpoint{{ApiInfo: v, Layers: []string{"FULLNODE_API_INFO"}}}, nil
	}

	type minimalApiInfo struct {
		Apis struct {
			ChainApiInfo []string
		}
	}

	addrToLayers := map[string][]string{}
	var configured []string
	seen := map[string]struct{}{}

	err := config.ForEachConfig[minimalApiInfo](ctx, db, func(name string, info minimalApiInfo) error {
		for _, addr := range info.Apis.ChainApiInfo {
			ai := parseAPIInfo(addr)
			if _, ok := seen[ai.Addr]; !ok {
				seen[ai.Addr] = struct{}{}
				configured = append(configured, addr)
			}
			addrToLayers[ai.Addr] = appendUnique(addrToLayers[ai.Addr], name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(configured) > 0 {
		out := make([]ChainRPCEndpoint, 0, len(configured))
		for _, addr := range configured {
			ai := parseAPIInfo(addr)
			out = append(out, ChainRPCEndpoint{
				ApiInfo: addr,
				Layers:  append([]string(nil), addrToLayers[ai.Addr]...),
			})
		}
		sort.Slice(out, func(i, j int) bool {
			return parseAPIInfo(out[i].ApiInfo).Addr < parseAPIInfo(out[j].ApiInfo).Addr
		})
		return out, nil
	}

	return nil, nil
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}
