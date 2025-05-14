package http

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

var log = logging.Logger("mk12httphdlr")

// Redirector struct with a database connection
type MK12DealHandler struct {
	cfg            *config.CurioConfig
	db             *harmonydb.DB // Replace with your actual DB wrapper if different
	dm             *storage_market.CurioStorageDealMarket
	disabledMiners []address.Address
}

// NewMarketDealHandler creates a new Redirector with a database connection
func NewMK12DealHandler(db *harmonydb.DB, cfg *config.CurioConfig, dm *storage_market.CurioStorageDealMarket) (*MK12DealHandler, error) {
	var disabledMiners []address.Address
	for _, m := range cfg.Market.StorageMarketConfig.MK12.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		disabledMiners = append(disabledMiners, maddr)
	}
	return &MK12DealHandler{db: db, dm: dm, cfg: cfg, disabledMiners: disabledMiners}, nil
}

// Router sets up the route for the deal handling
func Router(mdh *MK12DealHandler) http.Handler {
	mux := chi.NewRouter()
	mux.Use(dealRateLimitMiddleware())
	mux.Post("/store", mdh.mk12deal)
	mux.Get("/ask", mdh.mk12ask)
	mux.Get("/status", mdh.mk12status)
	return mux
}

func (mdh *MK12DealHandler) mk12deal(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "application/cbor" {
		log.Errorf("invalid content type: %s", ct)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reader := http.MaxBytesReader(w, r.Body, 4<<20) // 4 MB
	var proposal mk12.DealParams
	err := proposal.UnmarshalCBOR(reader)
	if err != nil {
		log.Errorf("error unmarshaling cbor: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	rlog := log.With("id", proposal.DealUUID)
	rlog.Infow("received deal proposal")
	startExec := time.Now()

	var res mk12.ProviderDealRejectionInfo

	if lo.Contains(mdh.disabledMiners, proposal.ClientDealProposal.Proposal.Provider) {
		rlog.Infow("Deal rejected as libp2p is disabled for provider", "deal", proposal.DealUUID, "provider", proposal.ClientDealProposal.Proposal.Provider)
		res.Accepted = false
		res.Reason = "Libp2p is disabled for the provider"
	} else {
		// Start executing the deal.
		// Note: This method just waits for the deal to be accepted, it doesn't
		// wait for deal execution to complete.
		eres, err := mdh.dm.MK12Handler.ExecuteDeal(context.Background(), &proposal, "")
		rlog.Debugw("processed deal proposal accept")
		if err != nil {
			rlog.Warnw("deal proposal failed", "err", err, "reason", res.Reason)
		}

		res = *eres
	}

	// Log the response
	rlog.Infow("send deal proposal response",
		"id", proposal.DealUUID,
		"accepted", res.Accepted,
		"msg", res.Reason,
		"peer id", "",
		"client address", proposal.ClientDealProposal.Proposal.Client,
		"provider address", proposal.ClientDealProposal.Proposal.Provider,
		"piece cid", proposal.ClientDealProposal.Proposal.PieceCID.String(),
		"piece size", proposal.ClientDealProposal.Proposal.PieceSize,
		"verified", proposal.ClientDealProposal.Proposal.VerifiedDeal,
		"label", proposal.ClientDealProposal.Proposal.Label,
		"start epoch", proposal.ClientDealProposal.Proposal.StartEpoch,
		"end epoch", proposal.ClientDealProposal.Proposal.EndEpoch,
		"price per epoch", proposal.ClientDealProposal.Proposal.StoragePricePerEpoch,
		"duration", time.Since(startExec).String(),
	)

	buf := new(bytes.Buffer)

	err = cborutil.WriteCborRPC(buf, &mk12.DealResponse{Accepted: res.Accepted, Message: res.Reason})
	if err != nil {
		rlog.Warnw("generating deal response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/cbor")
	_, err = w.Write(buf.Bytes())
	if err != nil {
		rlog.Warnw("writing deal response", "err", err)
	}

}

func (mdh *MK12DealHandler) mk12ask(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "application/cbor" {
		log.Errorf("invalid content type: %s", ct)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reader := http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
	var req legacytypes.AskRequest
	err := req.UnmarshalCBOR(reader)
	if err != nil {
		log.Errorf("error unmarshaling cbor: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var resp legacytypes.AskResponse

	resp.Ask, err = mdh.dm.MK12Handler.GetAsk(context.Background(), req.Miner)
	if err != nil {
		log.Errorw("failed to get ask from storage provider", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	buf := new(bytes.Buffer)
	err = cborutil.WriteCborRPC(buf, &resp)
	if err != nil {
		log.Errorw("failed to marshal ask response", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/cbor")
	_, err = w.Write(buf.Bytes())
	if err != nil {
		log.Errorw("failed to write ask response", "err", err)
	}
}

func (mdh *MK12DealHandler) mk12status(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "application/cbor" {
		log.Errorf("invalid content type: %s", ct)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	reader := http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB

	var req mk12.DealStatusRequest
	err := req.UnmarshalCBOR(reader)
	if err != nil {
		log.Errorf("error unmarshaling cbor: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	rlog := log.With("id", req.DealUUID)
	ctx := r.Context()
	resp := mk12.GetDealStatus(ctx, mdh.db, req, rlog)
	rlog.Debugw("processed deal status request")
	buf := new(bytes.Buffer)
	err = cborutil.WriteCborRPC(buf, &resp)
	if err != nil {
		rlog.Warnw("failed to marshal deal status response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/cbor")
	_, err = w.Write(buf.Bytes())
	if err != nil {
		rlog.Warnw("failed to write deal status response", "err", err)
	}
}

func dealRateLimitMiddleware() func(http.Handler) http.Handler {
	return httprate.LimitByIP(50, 1*time.Second)
}
