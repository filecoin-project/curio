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
	anyUnreachable := false
	worst := "ok"
	for _, s := range states {
		if !s.Reachable {
			anyUnreachable = true
			continue
		}
		reachable++
		syncState := strings.ToLower(s.SyncState)
		switch {
		case syncState == "ok":
		case strings.HasPrefix(syncState, "behind"):
			worst = "behind"
		case strings.HasPrefix(syncState, "slow") && worst == "ok":
			worst = "slow"
		case worst == "ok":
			worst = "degraded"
		}
	}
	if reachable == 0 {
		return "unreachable"
	}
	if worst != "ok" {
		return worst
	}
	if anyUnreachable {
		return "degraded"
	}
	return "ok"
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
