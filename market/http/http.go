package http

import (
	"github.com/go-chi/chi/v5"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"
	mk12http "github.com/filecoin-project/curio/market/mk12/http"
	mk20http "github.com/filecoin-project/curio/market/mk20/http"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/tasks/message"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

type MarketHandler struct {
	mdh12      *mk12http.MK12DealHandler
	mdh20      *mk20http.MK20DealHandler
	pdpService *pdp.PDPService
	domainName string
}

// NewMarketHandler is used to prepare all the required market handlers. Currently, it supports mk12 deal market.
// This function should be used to expand the functionality under "/market" path
func NewMarketHandler(db *harmonydb.DB, cfg *config.CurioConfig, dm *storage_market.CurioStorageDealMarket, eth api.EthClientInterface, fc pdp.PDPServiceNodeApi, sn *message.SenderETH, stor paths.StashStore) (*MarketHandler, error) {
	mdh12, err := mk12http.NewMK12DealHandler(db, cfg, dm)
	if err != nil {
		return nil, err
	}

	mdh20, err := mk20http.NewMK20DealHandler(db, cfg, dm)
	if err != nil {
		return nil, err
	}

	var pdpService *pdp.PDPService

	if sn != nil {
		pdpService = pdp.NewPDPService(db, stor, eth, fc, sn)
		//pdp.Routes(r, pdsvc)
	}

	return &MarketHandler{
		mdh12:      mdh12,
		mdh20:      mdh20,
		pdpService: pdpService,
		domainName: cfg.HTTP.DomainName,
	}, nil
}

// Router is used to attach all the market handlers
// This can include mk12 deals, mk20 deals(WIP), sector market(WIP) etc
func Router(mux *chi.Mux, mh *MarketHandler) {
	mux.Mount("/market/mk12", mk12http.Router(mh.mdh12))
	mux.Mount("/market/mk20", mk20http.Router(mh.mdh20, mh.domainName))
	if mh.pdpService != nil {
		mux.Mount("/market/pdp", pdp.Routes(mh.pdpService))
	}
	// TODO: Attach a info endpoint here with details about supported market modules and services under them
}
