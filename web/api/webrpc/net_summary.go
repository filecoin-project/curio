package webrpc

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/curio/api"

	cliutil "github.com/filecoin-project/lotus/cli/util"
)

type NetSummaryResponse struct {
	Epoch        int64               `json:"epoch"`
	PeerCount    int                 `json:"peerCount"`
	Bandwidth    NetBandwidthSummary `json:"bandwidth"`
	Reachability NetReachability     `json:"reachability"`
	NodeCount    int                 `json:"nodeCount"`
	Nodes        []NetNodeSummary    `json:"nodes"`
}

// NetNodeSummary is one sampled chain node for the Network panel.
type NetNodeSummary struct {
	Node         string              `json:"node"`
	Epoch        int64               `json:"epoch"`
	PeerCount    int                 `json:"peerCount"`
	Bandwidth    NetBandwidthSummary `json:"bandwidth"`
	Reachability NetReachability     `json:"reachability"`
}

type NetBandwidthSummary struct {
	TotalIn  int64   `json:"totalIn"`
	TotalOut int64   `json:"totalOut"`
	RateIn   float64 `json:"rateIn"`
	RateOut  float64 `json:"rateOut"`
}

type NetReachability struct {
	Status      string   `json:"status"`
	PublicAddrs []string `json:"publicAddrs"`
}

type nodeNetSample struct {
	node         string
	epoch        int64
	peerCount    int
	totalIn      int64
	totalOut     int64
	rateIn       float64
	rateOut      float64
	reachability string
	publicAddrs  []string
}

func (a *WebRPC) NetSummary(ctx context.Context) (NetSummaryResponse, error) {
	type minimalApiInfo struct {
		Apis struct {
			ChainApiInfo []string
		}
	}

	var rpcInfos []string
	err := forEachConfig[minimalApiInfo](a, func(_ string, info minimalApiInfo) error {
		rpcInfos = append(rpcInfos, info.Apis.ChainApiInfo...)
		return nil
	})
	if err != nil {
		return NetSummaryResponse{}, err
	}

	type sampleRes struct {
		s  nodeNetSample
		ok bool
	}
	resCh := make(chan sampleRes, len(rpcInfos))

	dedup := map[string]struct{}{}
	var wg sync.WaitGroup

	for _, info := range rpcInfos {
		ai := cliutil.ParseApiInfo(info)
		if _, ok := dedup[ai.Addr]; ok {
			continue
		}
		dedup[ai.Addr] = struct{}{}

		wg.Add(1)
		go func(ai cliutil.APIInfo) {
			defer wg.Done()

			addr, err := ai.DialArgs("v1")
			if err != nil {
				log.Warnw("NetSummary: DialArgs failed", "addr", ai.Addr, "error", err)
				return
			}

			var res api.ChainStruct
			closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin", api.GetInternalStructs(&res), ai.AuthHeader(), []jsonrpc.Option{jsonrpc.WithErrors(jsonrpc.NewErrors())}...)
			if err != nil {
				log.Warnw("NetSummary: NewMergeClient failed", "addr", ai.Addr, "error", err)
				return
			}
			defer closer()

			head, err := res.ChainHead(ctx)
			if err != nil {
				log.Warnw("NetSummary: ChainHead failed", "addr", ai.Addr, "error", err)
				return
			}

			peers, err := res.NetPeers(ctx)
			if err != nil {
				log.Warnw("NetSummary: NetPeers failed", "addr", ai.Addr, "error", err)
				return
			}

			bw, err := res.NetBandwidthStats(ctx)
			if err != nil {
				log.Warnw("NetSummary: NetBandwidthStats failed", "addr", ai.Addr, "error", err)
				return
			}

			nat, err := res.NetAutoNatStatus(ctx)
			if err != nil {
				log.Warnw("NetSummary: NetAutoNatStatus failed", "addr", ai.Addr, "error", err)
				return
			}

			resCh <- sampleRes{ok: true, s: nodeNetSample{
				node:         ai.Addr,
				epoch:        int64(head.Height()),
				peerCount:    len(peers),
				totalIn:      int64(bw.TotalIn),
				totalOut:     int64(bw.TotalOut),
				rateIn:       bw.RateIn,
				rateOut:      bw.RateOut,
				reachability: nat.Reachability.String(),
				publicAddrs:  nat.PublicAddrs,
			}}
		}(ai)
	}

	wg.Wait()
	close(resCh)

	summary := NetSummaryResponse{}
	pubAddrsSet := map[string]struct{}{}
	for r := range resCh {
		if !r.ok {
			continue
		}
		s := r.s
		nodeReachability := strings.ToLower(strings.TrimSpace(s.reachability))
		if nodeReachability == "" {
			nodeReachability = "unknown"
		}

		node := NetNodeSummary{
			Node:      s.node,
			Epoch:     s.epoch,
			PeerCount: s.peerCount,
			Bandwidth: NetBandwidthSummary{
				TotalIn:  s.totalIn,
				TotalOut: s.totalOut,
				RateIn:   s.rateIn,
				RateOut:  s.rateOut,
			},
			Reachability: NetReachability{
				Status:      nodeReachability,
				PublicAddrs: append([]string(nil), s.publicAddrs...),
			},
		}
		summary.Nodes = append(summary.Nodes, node)

		if s.epoch > summary.Epoch {
			summary.Epoch = s.epoch
		}
		summary.PeerCount += s.peerCount
		summary.Bandwidth.TotalIn += s.totalIn
		summary.Bandwidth.TotalOut += s.totalOut
		summary.Bandwidth.RateIn += s.rateIn
		summary.Bandwidth.RateOut += s.rateOut
		for _, a := range s.publicAddrs {
			pubAddrsSet[a] = struct{}{}
		}
	}

	summary.NodeCount = len(summary.Nodes)
	if summary.NodeCount > 0 {
		summary.Bandwidth.RateIn = summary.Bandwidth.RateIn / float64(summary.NodeCount)
		summary.Bandwidth.RateOut = summary.Bandwidth.RateOut / float64(summary.NodeCount)
	}

	pub, priv, unknown := 0, 0, 0
	for _, n := range summary.Nodes {
		switch {
		case strings.Contains(n.Reachability.Status, "public"):
			pub++
		case strings.Contains(n.Reachability.Status, "private"):
			priv++
		default:
			unknown++
		}
	}
	switch {
	case pub > 0:
		summary.Reachability.Status = "public"
	case priv > 0:
		summary.Reachability.Status = "private"
	default:
		summary.Reachability.Status = "unknown"
	}

	for a := range pubAddrsSet {
		summary.Reachability.PublicAddrs = append(summary.Reachability.PublicAddrs, a)
	}
	sort.Strings(summary.Reachability.PublicAddrs)
	sort.Slice(summary.Nodes, func(i, j int) bool {
		return summary.Nodes[i].Node < summary.Nodes[j].Node
	})

	return summary, nil
}
