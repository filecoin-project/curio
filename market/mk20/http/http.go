package http

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	logging "github.com/ipfs/go-log/v2"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

//go:embed info.md
var infoMarkdown []byte

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
	mux.Method("POST", "/store", http.TimeoutHandler(http.HandlerFunc(mdh.mk20deal), 10*time.Second, "timeout reading request"))
	mux.Method("GET", "/status", http.TimeoutHandler(http.HandlerFunc(mdh.mk20status), 10*time.Second, "timeout reading request"))
	mux.Method("GET", "/contracts", http.TimeoutHandler(http.HandlerFunc(mdh.mk20supportedContracts), 10*time.Second, "timeout reading request"))
	mux.Put("/data", mdh.mk20UploadDealData)
	mux.Method("GET", "/info", http.TimeoutHandler(http.HandlerFunc(mdh.info), 10*time.Second, "timeout reading request"))
	//mux.Post("/store", mdh.mk20deal)
	//mux.Get("/status", mdh.mk20status)
	//mux.Get("/contracts", mdh.mk20supportedContracts)
	//mux.Get("/info", mdh.info)
	return mux
}

// mk20deal handles incoming HTTP POST requests to process MK20 deals.
// It validates the request's content type and body, then parses and executes the deal logic.
// Responds with appropriate HTTP status codes and logs detailed information about the process.
func (mdh *MK20DealHandler) mk20deal(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	var deal mk20.Deal
	if ct != "application/json" {
		log.Errorf("invalid content type: %s", ct)
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("error reading request body: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Infow("received deal proposal", "body", string(body))

	err = json.Unmarshal(body, &deal)
	if err != nil {
		log.Errorf("error unmarshaling json: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Infow("received deal proposal", "deal", deal)

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

// mk20status handles HTTP requests to retrieve the status of a deal using its ID, responding with deal status or appropriate error codes.
func (mdh *MK20DealHandler) mk20status(w http.ResponseWriter, r *http.Request) {
	// Extract id from the URL
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		log.Errorw("missing id in url", "url", r.URL)
		http.Error(w, "missing id in url", http.StatusBadRequest)
		return
	}
	id, err := ulid.Parse(idStr)
	if err != nil {
		log.Errorw("invalid id in url", "id", idStr, "err", err)
		http.Error(w, "invalid id in url", http.StatusBadRequest)
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

// mk20supportedContracts retrieves supported contract addresses from the database and returns them as a JSON response.
func (mdh *MK20DealHandler) mk20supportedContracts(w http.ResponseWriter, r *http.Request) {
	var contracts mk20.SupportedContracts
	err := mdh.db.Select(r.Context(), &contracts, "SELECT address FROM contracts")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			log.Errorw("no supported contracts found")
			http.Error(w, "no supported contracts found", http.StatusNotFound)
			return
		}
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

// mk20UploadDealData handles uploading deal data to the server using a PUT request with specific validations and streams directly to the logic.
func (mdh *MK20DealHandler) mk20UploadDealData(w http.ResponseWriter, r *http.Request) {
	// Extract id from the URL
	idStr := r.URL.Query().Get("id")
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
	mdh.dm.MK20Handler.HandlePutRequest(id, r.Body, w)
}

// info serves the contents of the info file as a text/markdown response with HTTP 200 or returns an HTTP 500 on read/write failure.
func (mdh *MK20DealHandler) info(w http.ResponseWriter, r *http.Request) {

	var mdRenderer = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Linkify,
			extension.Table,
			extension.DefinitionList,
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
	)

	var buf bytes.Buffer
	if err := mdRenderer.Convert(infoMarkdown, &buf); err != nil {
		http.Error(w, "failed to render markdown", http.StatusInternalServerError)
		return
	}
	//if err := goldmark.Convert(infoMarkdown, &buf); err != nil {
	//	http.Error(w, "failed to render markdown", http.StatusInternalServerError)
	//	return
	//}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	rendered := strings.ReplaceAll(buf.String(), "<table>", `<table class="table table-dark table-striped table-sm table-bordered">`)

	htmlStr := fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<title>Curio Deal Schema</title>
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.6/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-4Q6Gf2aSP4eDXB8Miphtr37CMZZQ5oXLH2yaXMJ2w8e2ZtHTl7GptT4jmndRuHDT" crossorigin="anonymous">
	<style>
		body {
			background-color: #f8f9fa;
			padding: 2rem;
		}
		pre, code {
			background-color: #f1f3f5;
			border-radius: 4px;
			padding: 0.25em 0.5em;
		}
		table {
			margin-top: 1rem;
		}
	</style>
</head>
<body>
<div class="container">
%s
</div>
</body>
</html>`, rendered)

	_, err := w.Write([]byte(htmlStr))
	if err != nil {
		log.Errorw("failed to write info file", "err", err)
	}
}
