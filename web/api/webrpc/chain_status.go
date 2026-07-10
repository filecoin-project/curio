package webrpc

import (
	"context"
	"strings"

	"github.com/filecoin-project/curio/build"
)

type ChainStatusResponse struct {
	NetworkName    string `json:"networkName"`
	Epoch          int64  `json:"epoch"`
	SyncStatus     string `json:"syncStatus"`
	ReachableNodes int    `json:"reachableNodes"`
	TotalNodes     int    `json:"totalNodes"`
}

func networkNameFromBuild() string {
	s := build.BuildTypeString()
	if len(s) > 0 && s[0] == '+' {
		return s[1:]
	}
	return s
}

func rollupSyncStatus(states []RpcInfo) string {
	if len(states) == 0 {
		return "unknown"
	}

	reachable := 0
	allOk := true
	worst := "ok"

	for _, s := range states {
		if !s.Reachable {
			worst = "unreachable"
			continue
		}
		reachable++
		if s.SyncState != "ok" {
			allOk = false
			switch {
			case strings.HasPrefix(s.SyncState, "behind"):
				worst = "behind"
			case worst == "ok" && strings.HasPrefix(s.SyncState, "slow"):
				worst = "slow"
			}
		}
	}

	if reachable == 0 {
		return "unreachable"
	}
	if allOk {
		return "ok"
	}
	if worst == "unreachable" && reachable > 0 {
		return "degraded"
	}
	return worst
}

func (a *WebRPC) ChainStatus(ctx context.Context) (ChainStatusResponse, error) {
	net, err := a.NetSummary(ctx)
	if err != nil {
		return ChainStatusResponse{}, err
	}

	states, err := a.SyncerState(ctx)
	if err != nil {
		return ChainStatusResponse{}, err
	}

	reachable := 0
	for _, s := range states {
		if s.Reachable {
			reachable++
		}
	}

	return ChainStatusResponse{
		NetworkName:    networkNameFromBuild(),
		Epoch:          net.Epoch,
		SyncStatus:     rollupSyncStatus(states),
		ReachableNodes: reachable,
		TotalNodes:     len(states),
	}, nil
}
