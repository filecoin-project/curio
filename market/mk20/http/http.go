package http

import (
	"context"
	"embed"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	logging "github.com/ipfs/go-log/v2"
	"github.com/oklog/ulid"
	httpSwagger "github.com/swaggo/http-swagger/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

//go:embed swagger.yaml swagger.json docs.go
var swaggerAssets embed.FS

var log = logging.Logger("mk20httphdlr")

const version = "1.0.0"

const requestTimeout = 10 * time.Second

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

func AuthMiddleware(db *harmonydb.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
				return
			}

			allowed, client, err := mk20.Auth(authHeader, r.URL.Path, db)
			if err != nil {
				log.Errorw("failed to authenticate request", "err", err)
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			if !allowed {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			idStr := chi.URLParam(r, "id")
			if idStr != "" {
				allowed, err := mk20.AuthenticateClient(db, idStr, client)
				if err != nil {
					log.Errorw("failed to authenticate client", "err", err)
					http.Error(w, err.Error(), http.StatusUnauthorized)
					return
				}
				if !allowed {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// @title Curio Market 2.0 API
// @description Curio market APIs
func Router(mdh *MK20DealHandler, domainName string) http.Handler {
	mux := chi.NewRouter()
	mux.Use(dealRateLimitMiddleware())
	mux.Mount("/", APIRouter(mdh, domainName))
	mux.Mount("/info", InfoRouter())
	return mux
}

// @securityDefinitions.apikey CurioAuth
// @in header
// @name Authorization
// @description Use the format: `CurioAuth PublicKeyType:PublicKey:Signature`
// @description
// @description - `PublicKeyType`: String representation of type of wallet (e.g., "ed25519", "bls", "secp256k1")
// @description - `PublicKey`: Base64 string of public key bytes
// @description - `Signature`: Signature is Base64 string of signature bytes.
// @description - The client is expected to sign the SHA-256 hash of a message constructed by concatenating the following three components, in order.
// @description - The raw public key bytes (not a human-readable address)
// @description - The HTTP request path, such as /user/info
// @description - The timestamp, truncated to the nearest minute, formatted in RFC3339 (e.g., 2025-07-15T17:42:00Z)
// @description - These three byte slices are joined without any delimiter between them, and the resulting byte array is then hashed using SHA-256. The signature is performed on that hash.
// @security CurioAuth
func APIRouter(mdh *MK20DealHandler, domainName string) http.Handler {
	SwaggerInfo.BasePath = "/market/mk20"
	SwaggerInfo.Host = fmt.Sprintf("https://%s", domainName)
	SwaggerInfo.Version = version
	mux := chi.NewRouter()
	mux.Use(dealRateLimitMiddleware())
	mux.Use(AuthMiddleware(mdh.db))
	mux.Method("POST", "/store", http.TimeoutHandler(http.HandlerFunc(mdh.mk20deal), requestTimeout, "request timeout"))
	mux.Method("GET", "/status/{id}", http.TimeoutHandler(http.HandlerFunc(mdh.mk20status), requestTimeout, "request timeout"))
	mux.Method("GET", "/contracts", http.TimeoutHandler(http.HandlerFunc(mdh.mk20supportedContracts), requestTimeout, "request timeout"))
	mux.Method("POST", "/upload/{id}", http.TimeoutHandler(http.HandlerFunc(mdh.mk20UploadStart), requestTimeout, "request timeout"))
	mux.Method("GET", "/upload/{id}", http.TimeoutHandler(http.HandlerFunc(mdh.mk20UploadStatus), requestTimeout, "request timeout"))
	mux.Put("/upload/{id}/{chunkNum}", mdh.mk20UploadDealChunks)
	mux.Method("POST", "/upload/finalize/{id}", http.TimeoutHandler(http.HandlerFunc(mdh.mk20FinalizeUpload), requestTimeout, "request timeout"))
	mux.Method("GET", "/products", http.TimeoutHandler(http.HandlerFunc(mdh.supportedProducts), requestTimeout, "request timeout"))
	mux.Method("GET", "/sources", http.TimeoutHandler(http.HandlerFunc(mdh.supportedDataSources), requestTimeout, "request timeout"))
	mux.Method("POST", "/update/{id}", http.TimeoutHandler(http.HandlerFunc(mdh.mk20UpdateDeal), requestTimeout, "request timeout"))
	return mux
}

// InfoRouter serves OpenAPI specs and OpenAPI info
func InfoRouter() http.Handler {
	mux := chi.NewRouter()
	mux.Get("/*", httpSwagger.Handler())

	mux.Get("/swagger.yaml", func(w http.ResponseWriter, r *http.Request) {
		swaggerYAML, err := swaggerAssets.ReadFile("swagger.yaml")
		if err != nil {
			log.Errorw("failed to read swagger.yaml", "err", err)
			http.Error(w, "failed to read swagger.yaml", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-yaml")
		_, _ = w.Write(swaggerYAML)
	})

	mux.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
		swaggerJSON, err := swaggerAssets.ReadFile("swagger.json")
		if err != nil {
			log.Errorw("failed to read swagger.json", "err", err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(swaggerJSON)
	})
	return mux
}

// mk20deal handles HTTP requests to process MK20 deals, parses the request body, validates it, and executes the deal logic.
// @Router /store [post]
// @Summary Make a mk20 deal
// @Description Make a mk20 deal
// @BasePath /market/mk20
// @Param body body mk20.Deal true "mk20.Deal in json format"
// @Accept json
// @Failure 200 {object} mk20.DealCode "Ok represents a successful operation with an HTTP status code of 200"
// @Failure 400 {object} mk20.DealCode "ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic"
// @Failure 404 {object} mk20.DealCode "ErrDealNotFound indicates that the specified deal could not be found, corresponding to the HTTP status code 404"
// @Failure 430 {object} mk20.DealCode "ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data"
// @Failure 422 {object} mk20.DealCode "ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context"
// @Failure 423 {object} mk20.DealCode "ErrUnsupportedProduct indicates that the requested product is not supported by the provider"
// @Failure 424 {object} mk20.DealCode "ErrProductNotEnabled indicates that the requested product is not enabled on the provider"
// @Failure 425 {object} mk20.DealCode "ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data"
// @Failure 426 {object} mk20.DealCode "ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules"
// @Failure 500 {object} mk20.DealCode "ErrServerInternalError indicates an internal server error with a corresponding error code of 500"
// @Failure 503 {object} mk20.DealCode "ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503"
// @Failure 429 {object} mk20.DealCode "ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment"
// @Failure 440 {object} mk20.DealCode "ErrMarketNotEnabled indicates that the market is not enabled for the requested operation"
// @Failure 441 {object} mk20.DealCode "ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
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

	w.WriteHeader(int(result.HTTPCode))
	_, err = w.Write([]byte(fmt.Sprint("Reason: ", result.Reason)))
	if err != nil {
		log.Errorw("writing deal response:", "id", deal.Identifier, "error", err)
	}
}

// mk20status handles HTTP requests to fetch the status of a deal by its ID and responding with JSON-encoded results.
// @Router /status/{id} [get]
// @Summary List of supported DDO contracts
// @Description List of supported DDO contracts
// @BasePath /market/mk20
// @Param id path string true "id"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
// @Failure 200 {string} string "OK - Success"
// @Failure 500 {string} string "Internal Server Error"
func (mdh *MK20DealHandler) mk20status(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
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

// mk20supportedContracts handles HTTP requests to retrieve supported contract addresses and returns them in a JSON response.
// @Router /contracts [get]
// @Summary List of supported DDO contracts
// @Description List of supported DDO contracts
// @BasePath /market/mk20
// @Failure 500 {string} string "Internal Server Error"
// @Failure 200 {string} string "OK - Success"
func (mdh *MK20DealHandler) mk20supportedContracts(w http.ResponseWriter, r *http.Request) {
	var contracts mk20.SupportedContracts
	err := mdh.db.Select(r.Context(), &contracts, "SELECT address FROM ddo_contracts")
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

// supportedProducts handles HTTP requests to retrieve a list of supported MK20 products and returns them in a JSON response.
// @Router /products [get]
// @Summary List of supported products
// @Description List of supported products
// @BasePath /market/mk20
// @Failure 500 {string} string "Internal Server Error"
// @Failure 200 {string} string "OK - Success"
func (mdh *MK20DealHandler) supportedProducts(w http.ResponseWriter, r *http.Request) {
	prods, _, err := mdh.dm.MK20Handler.Supported(r.Context())
	if err != nil {
		log.Errorw("failed to get supported producers and sources", "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	var products mk20.SupportedProducts
	for k, v := range prods {
		if v {
			products.Products = append(products.Products, k)
		}
	}
	resp, err := json.Marshal(products)
	if err != nil {
		log.Errorw("failed to marshal supported products", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(resp)
	if err != nil {
		log.Errorw("failed to write supported products", "err", err)
	}
}

// supportedDataSources handles HTTP requests to retrieve the supported data sources in JSON format.
// @Router /sources [get]
// @Summary List of supported dats sources
// @Description List of supported data sources
// @BasePath /market/mk20
// @Failure 500 {string} string "Internal Server Error"
// @Failure 200 {string} string "OK - Success"
func (mdh *MK20DealHandler) supportedDataSources(w http.ResponseWriter, r *http.Request) {
	_, srcs, err := mdh.dm.MK20Handler.Supported(r.Context())
	if err != nil {
		log.Errorw("failed to get supported producers and sources", "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	var sources mk20.SupportedDataSources
	for k, v := range srcs {
		if v {
			sources.Sources = append(sources.Sources, k)
		}
	}
	resp, err := json.Marshal(sources)
	if err != nil {
		log.Errorw("failed to marshal supported sources", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(resp)
	if err != nil {
		log.Errorw("failed to write supported sources", "err", err)
	}
}

// mk20UploadStatus handles the upload status requests for a given id.
// @Router /upload/{id} [get]
// @Param id path string true "id"
// @Summary Status of deal upload
// @Description Return a json struct detailing the current status of a deal upload.
// @BasePath /market/mk20
// @Failure 200 {object} mk20.UploadStatusCode "UploadStatusCodeOk represents a successful upload operation with status code 200"
// @Failure 404 {object} mk20.UploadStatusCode "UploadStatusCodeDealNotFound indicates that the requested deal was not found, corresponding to status code 404"
// @Failure 425 {object} mk20.UploadStatusCode "UploadStatusCodeUploadNotStarted indicates that the upload process has not started yet"
// @Failure 500 {object} mk20.UploadStatusCode "UploadStatusCodeServerError indicates an internal server error occurred during the upload process, corresponding to status code 500"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
func (mdh *MK20DealHandler) mk20UploadStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
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
	mdh.dm.MK20Handler.HandleUploadStatus(r.Context(), id, w)
}

// mk20UploadDealChunks handles uploading of deal file chunks.
// @Router /upload/{id}/{chunkNum} [put]
// @Summary Upload a file chunk
// @Description Allows uploading chunks for a deal file. Method can be called in parallel to speed up uploads.
// @BasePath /market/mk20
// @Param id path string true "id"
// @Param chunkNum path string true "chunkNum"
// @accepts bytes
// @Param body body []byte true "raw binary"
// @Failure 200 {object} mk20.UploadCode "UploadOk indicates a successful upload operation, represented by the HTTP status code 200"
// @Failure 400 {object} mk20.UploadCode "UploadBadRequest represents a bad request error with an HTTP status code of 400"
// @Failure 404 {object} mk20.UploadCode "UploadNotFound represents an error where the requested upload chunk could not be found, typically corresponding to HTTP status 404"
// @Failure 409 {object} mk20.UploadCode "UploadChunkAlreadyUploaded indicates that the chunk has already been uploaded and cannot be re-uploaded"
// @Failure 500 {object} mk20.UploadCode "UploadServerError indicates a server-side error occurred during the upload process, represented by the HTTP status code 500"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
func (mdh *MK20DealHandler) mk20UploadDealChunks(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "application/octet-stream" {
		log.Errorw("invalid content type", "ct", ct)
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	idStr := chi.URLParam(r, "id")
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

	chunk := chi.URLParam(r, "chunkNum")
	if chunk == "" {
		log.Errorw("missing chunk number in url", "url", r.URL)
		http.Error(w, "missing chunk number in url", http.StatusBadRequest)
		return
	}

	chunkNum, err := strconv.Atoi(chunk)
	if err != nil {
		log.Errorw("invalid chunk number in url", "url", r.URL)
		http.Error(w, "invalid chunk number in url", http.StatusBadRequest)
		return
	}

	mdh.dm.MK20Handler.HandleUploadChunk(id, chunkNum, r.Body, w)
}

// mk20UploadStart handles the initiation of an upload process for MK20 deal data.
// @Router /upload/{id} [post]
// @Summary Starts the upload process
// @Description Initializes the upload for a deal. Each upload must be initialized before chunks can be uploaded for a deal.
// @BasePath /market/mk20
// @Param id path string true "id"
// @Failure 200 {object} mk20.UploadStartCode "UploadStartCodeOk indicates a successful upload start request with status code 200"
// @Failure 400 {object} mk20.UploadStartCode "UploadStartCodeBadRequest indicates a bad upload start request error with status code 400"
// @Failure 404 {object} mk20.UploadStartCode "UploadStartCodeDealNotFound represents a 404 status indicating the deal was not found during the upload start process"
// @Failure 409 {object} mk20.UploadStartCode "UploadStartCodeAlreadyStarted indicates that the upload process has already been initiated and cannot be started again"
// @Failure 500 {object} mk20.UploadStartCode "UploadStartCodeServerError indicates an error occurred on the server while processing an upload start request"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
func (mdh *MK20DealHandler) mk20UploadStart(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "application/json" {
		log.Errorw("invalid content type", "ct", ct)
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	idStr := chi.URLParam(r, "id")
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

	reader := io.LimitReader(r.Body, 4*1024*1024)
	b, err := io.ReadAll(reader)
	if err != nil {
		log.Errorw("failed to read request body", "err", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	upload := mk20.StartUpload{}
	err = json.Unmarshal(b, &upload)
	if err != nil {
		log.Errorw("failed to unmarshal request body", "err", err)
		http.Error(w, "failed to unmarshal request body", http.StatusBadRequest)
		return
	}

	mdh.dm.MK20Handler.HandleUploadStart(r.Context(), id, upload, w)

}

// mk20FinalizeUpload finalizes the upload process for a given deal by processing the request and updating the associated deal in the system if required.
// @Router /upload/finalize/{id} [post]
// @Summary Finalizes the upload process
// @Description Finalizes the upload process once all the chunks are uploaded.
// @BasePath /market/mk20
// @Param id path string true "id"
// @accepts json
// @Param body body mk20.Deal optional "mk20.deal in json format"
// @Accept json
// @Failure 200 {object} mk20.DealCode "Ok represents a successful operation with an HTTP status code of 200"
// @Failure 400 {object} mk20.DealCode "ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic"
// @Failure 404 {object} mk20.DealCode "ErrDealNotFound indicates that the specified deal could not be found, corresponding to the HTTP status code 404"
// @Failure 430 {object} mk20.DealCode "ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data"
// @Failure 422 {object} mk20.DealCode "ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context"
// @Failure 423 {object} mk20.DealCode "ErrUnsupportedProduct indicates that the requested product is not supported by the provider"
// @Failure 424 {object} mk20.DealCode "ErrProductNotEnabled indicates that the requested product is not enabled on the provider"
// @Failure 425 {object} mk20.DealCode "ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data"
// @Failure 426 {object} mk20.DealCode "ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules"
// @Failure 500 {object} mk20.DealCode "ErrServerInternalError indicates an internal server error with a corresponding error code of 500"
// @Failure 503 {object} mk20.DealCode "ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503"
// @Failure 429 {object} mk20.DealCode "ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment"
// @Failure 440 {object} mk20.DealCode "ErrMarketNotEnabled indicates that the market is not enabled for the requested operation"
// @Failure 441 {object} mk20.DealCode "ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
func (mdh *MK20DealHandler) mk20FinalizeUpload(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
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

	ct := r.Header.Get("Content-Type")
	// If Content-Type is not set this is does not require updating the deal
	if len(ct) == 0 {
		log.Infow("received finalize upload proposal without content type", "id", id)
		mdh.dm.MK20Handler.HandleUploadFinalize(id, nil, w)
		return
	}

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

	log.Infow("received upload finalize proposal", "body", string(body))

	err = json.Unmarshal(body, &deal)
	if err != nil {
		log.Errorf("error unmarshaling json: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mdh.dm.MK20Handler.HandleUploadFinalize(id, &deal, w)
}

// mk20UpdateDeal handles updating an MK20 deal based on the provided HTTP request.
// It validates the deal ID, request content type, and JSON body before updating.
// @Summary Update the deal details of existing deals.
// @Description Useful for adding adding additional products and updating PoRep duration
// @BasePath /market/mk20
// @Router /update/{id} [get]
// @Param id path string true "id"
// @Accept json
// @Param body body mk20.Deal true "mk20.Deal in json format"
// @Failure 200 {object} mk20.DealCode "Ok represents a successful operation with an HTTP status code of 200"
// @Failure 400 {object} mk20.DealCode "ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic"
// @Failure 404 {object} mk20.DealCode "ErrDealNotFound indicates that the specified deal could not be found, corresponding to the HTTP status code 404"
// @Failure 430 {object} mk20.DealCode "ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data"
// @Failure 422 {object} mk20.DealCode "ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context"
// @Failure 423 {object} mk20.DealCode "ErrUnsupportedProduct indicates that the requested product is not supported by the provider"
// @Failure 424 {object} mk20.DealCode "ErrProductNotEnabled indicates that the requested product is not enabled on the provider"
// @Failure 425 {object} mk20.DealCode "ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data"
// @Failure 426 {object} mk20.DealCode "ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules"
// @Failure 500 {object} mk20.DealCode "ErrServerInternalError indicates an internal server error with a corresponding error code of 500"
// @Failure 503 {object} mk20.DealCode "ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503"
// @Failure 429 {object} mk20.DealCode "ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment"
// @Failure 440 {object} mk20.DealCode "ErrMarketNotEnabled indicates that the market is not enabled for the requested operation"
// @Failure 441 {object} mk20.DealCode "ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold"
// @Failure 400 {string} string "Bad Request - Invalid input or validation error"
func (mdh *MK20DealHandler) mk20UpdateDeal(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		log.Errorw("missing id in url", "url", r.URL)
		http.Error(w, "missing id in url", http.StatusBadRequest)
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		log.Errorw("invalid id in url", "id", idStr, "err", err)
		http.Error(w, "invalid id in url", http.StatusBadRequest)
		return
	}

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

	err = json.Unmarshal(body, &deal)
	if err != nil {
		log.Errorf("error unmarshaling json: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Infow("received deal update proposal", "body", string(body))

	mdh.dm.MK20Handler.UpdateDeal(id, &deal, w)
}
