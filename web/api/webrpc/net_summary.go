package webrpc

import (
	"context"
	"sort"
	"strconv"
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
		s   nodeNetSample
		ok  bool
		err error
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
				resCh <- sampleRes{err: err}
				return
			}

			var res api.ChainStruct
			closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin", api.GetInternalStructs(&res), ai.AuthHeader(), []jsonrpc.Option{jsonrpc.WithErrors(jsonrpc.NewErrors())}...)
			if err != nil {
				resCh <- sampleRes{err: err}
				return
			}
			defer closer()

			head, err := res.ChainHead(ctx)
			if err != nil {
				resCh <- sampleRes{err: err}
				return
			}

			peers, err := res.NetPeers(ctx)
			if err != nil {
				resCh <- sampleRes{err: err}
				return
			}

			bw, err := res.NetBandwidthStats(ctx)
			if err != nil {
				resCh <- sampleRes{err: err}
				return
			}

			nat, err := res.NetAutoNatStatus(ctx)
			if err != nil {
				resCh <- sampleRes{err: err}
				return
			}

			resCh <- sampleRes{ok: true, s: nodeNetSample{
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
	var reach []string
	pubAddrsSet := map[string]struct{}{}
	okCount := 0
	for r := range resCh {
		if !r.ok {
			continue
		}
		okCount++
		s := r.s
		if s.epoch > summary.Epoch {
			summary.Epoch = s.epoch
		}
		summary.PeerCount += s.peerCount
		summary.Bandwidth.TotalIn += s.totalIn
		summary.Bandwidth.TotalOut += s.totalOut
		summary.Bandwidth.RateIn += s.rateIn
		summary.Bandwidth.RateOut += s.rateOut
		if s.reachability != "" {
			reach = append(reach, strings.ToLower(s.reachability))
		}
		for _, a := range s.publicAddrs {
			pubAddrsSet[a] = struct{}{}
		}
	}

	summary.NodeCount = okCount
	if okCount > 0 {
		summary.Bandwidth.RateIn = summary.Bandwidth.RateIn / float64(okCount)
		summary.Bandwidth.RateOut = summary.Bandwidth.RateOut / float64(okCount)
	}

	if len(reach) == 0 {
		summary.Reachability.Status = "unknown"
	} else {
		pub, priv, unknown := 0, 0, 0
		for _, r := range reach {
			switch {
			case strings.Contains(r, "public"):
				pub++
			case strings.Contains(r, "private"):
				priv++
			default:
				unknown++
			}
		}
		summary.Reachability.Status = "public=" + itoa(pub) + ", private=" + itoa(priv) + ", unknown=" + itoa(unknown)
	}

	for a := range pubAddrsSet {
		summary.Reachability.PublicAddrs = append(summary.Reachability.PublicAddrs, a)
	}
	sort.Strings(summary.Reachability.PublicAddrs)

	return summary, nil
}

func itoa(v int) string { return strconv.Itoa(v) }
