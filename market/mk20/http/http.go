package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	logging "github.com/ipfs/go-log/v2"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

var log = logging.Logger("mk20httphdlr")

const maxPutBodySize int64 = 64 << 30 // 64 GiB

type MK20DealHandler struct {
	cfg            *config.CurioConfig
	db             *harmonydb.DB // Replace with your actual DB wrapper if different
	dm             *storage_market.CurioStorageDealMarket
	disabledMiners []address.Address
}

func NewMK20DealHandler(db *harmonydb.DB, cfg *config.CurioConfig, dm *storage_market.CurioStorageDealMarket) (*MK20DealHandler, error) {
	var disabledMiners []address.Address
	for _, m := range cfg.Market.StorageMarketConfig.MK12.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		disabledMiners = append(disabledMiners, maddr)
	}
	return &MK20DealHandler{db: db, dm: dm, cfg: cfg, disabledMiners: disabledMiners}, nil
}

func dealRateLimitMiddleware() func(http.Handler) http.Handler {
	return httprate.LimitByIP(50, 1*time.Second)
}

func Router(mdh *MK20DealHandler) http.Handler {
	mux := chi.NewRouter()
	mux.Use(dealRateLimitMiddleware())
	mux.Post("/store", mdh.mk20deal)
	//mux.Get("/ask", mdh.mk20ask)
	mux.Get("/status", mdh.mk20status)
	mux.Get("/contracts", mdh.mk20supportedContracts)
	mux.Put("/data", mdh.mk20UploadDealData)
	return mux
}

func (mdh *MK20DealHandler) mk20deal(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	var deal mk20.Deal
	if ct != "application/json" {
		log.Errorf("invalid content type: %s", ct)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("error reading request body: %s", err)
		w.WriteHeader(http.StatusBadRequest)
	}
	err = json.Unmarshal(body, &deal)
	if err != nil {
		log.Errorf("error unmarshaling json: %s", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	result := mdh.dm.MK20Handler.ExecuteDeal(context.Background(), &deal)

	log.Infow("deal processed",
		"id", deal.Identifier,
		"HTTPCode", result.HTTPCode,
		"Reason", result.Reason)

	w.WriteHeader(result.HTTPCode)
	_, err = w.Write([]byte(fmt.Sprint("Reason: ", result.Reason)))
	if err != nil {
		log.Errorw("writing deal response:", "id", deal.Identifier, "error", err)
	}
}

func (mdh *MK20DealHandler) mk20status(w http.ResponseWriter, r *http.Request) {
	// Extract id from the URL
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		log.Errorw("missing id in url", "url", r.URL)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	id, err := ulid.Parse(idStr)
	if err != nil {
		log.Errorw("invalid id in url", "id", idStr, "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	result := mdh.dm.MK20Handler.DealStatus(context.Background(), id)

	if result.HTTPCode != http.StatusOK {
		w.WriteHeader(result.HTTPCode)
		return
	}

	resp, err := json.Marshal(result.Response)
	if err != nil {
		log.Errorw("failed to marshal deal status response", "id", idStr, "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(resp)
	if err != nil {
		log.Errorw("failed to write deal status response", "id", idStr, "err", err)
	}
}

func (mdh *MK20DealHandler) mk20supportedContracts(w http.ResponseWriter, r *http.Request) {
	var contracts mk20.SupportedContracts
	err := mdh.db.Select(r.Context(), &contracts, "SELECT address FROM contracts")
	if err != nil {
		log.Errorw("failed to get supported contracts", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	// Write a json array
	resp, err := json.Marshal(contracts)
	if err != nil {
		log.Errorw("failed to marshal supported contracts", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(resp)
	if err != nil {
		log.Errorw("failed to write supported contracts", "err", err)
	}
}

func (mdh *MK20DealHandler) mk20UploadDealData(w http.ResponseWriter, r *http.Request) {
	// Extract id from the URL
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		log.Errorw("missing id in url", "url", r.URL)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		log.Errorw("invalid id in url", "id", idStr, "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Check Content-Type
	ct := r.Header.Get("Content-Type")
	if ct == "" || !strings.HasPrefix(ct, "application/octet-stream") {
		http.Error(w, "invalid or missing Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// validate Content-Length
	if r.ContentLength <= 0 || r.ContentLength > maxPutBodySize {
		http.Error(w, fmt.Sprintf("invalid Content-Length: %d", r.ContentLength), http.StatusRequestEntityTooLarge)
		return
	}

	// Stream directly to execution logic
	mdh.dm.MK20Handler.HandlePutRequest(context.Background(), id, r.Body, w)
}
