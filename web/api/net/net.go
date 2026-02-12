package net

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api/apihelper"
)

type netSummary struct {
	*deps.Deps
	mu       sync.Mutex
	cachedAt time.Time
	cached   summaryResp
}

type summaryResp struct {
	Epoch        int64               `json:"epoch"`
	PeerCount    int                 `json:"peerCount"`
	Bandwidth    bandwidthSummary    `json:"bandwidth"`
	Reachability reachabilitySummary `json:"reachability"`
}

type bandwidthSummary struct {
	TotalIn  int64   `json:"totalIn"`
	TotalOut int64   `json:"totalOut"`
	RateIn   float64 `json:"rateIn"`
	RateOut  float64 `json:"rateOut"`
}

type reachabilitySummary struct {
	Status      string   `json:"status"`
	PublicAddrs []string `json:"publicAddrs"`
}

func Routes(r *mux.Router, deps *deps.Deps) {
	h := &netSummary{Deps: deps}
	r.Methods("GET").Path("/summary").HandlerFunc(h.getSummary)
}

func (h *netSummary) getSummary(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if !h.cachedAt.IsZero() && time.Since(h.cachedAt) < 2*time.Second {
		cached := h.cached
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(cached))
		return
	}
	h.mu.Unlock()

	ctx := r.Context()
	head, err := h.Chain.ChainHead(ctx)
	apihelper.OrHTTPFail(w, err)

	peers, err := h.Chain.NetPeers(ctx)
	apihelper.OrHTTPFail(w, err)

	bw, err := h.Chain.NetBandwidthStats(ctx)
	apihelper.OrHTTPFail(w, err)

	nat, err := h.Chain.NetAutoNatStatus(ctx)
	apihelper.OrHTTPFail(w, err)

	resp := summaryResp{
		Epoch:     int64(head.Height()),
		PeerCount: len(peers),
		Bandwidth: bandwidthSummary{
			TotalIn:  int64(bw.TotalIn),
			TotalOut: int64(bw.TotalOut),
			RateIn:   bw.RateIn,
			RateOut:  bw.RateOut,
		},
		Reachability: reachabilitySummary{
			Status:      nat.Reachability.String(),
			PublicAddrs: nat.PublicAddrs,
		},
	}

	h.mu.Lock()
	h.cached = resp
	h.cachedAt = time.Now()
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	apihelper.OrHTTPFail(w, json.NewEncoder(w).Encode(resp))
}
