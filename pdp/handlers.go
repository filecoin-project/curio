package pdp

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/paths"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/indexing"
	"github.com/filecoin-project/curio/tasks/message"

	types2 "github.com/filecoin-project/lotus/chain/types"
)

func httpServerError(w http.ResponseWriter, statusCode int, msg string, err error) {
	if chainErr, ok := errors.AsType[*api.ChainError](err); ok {
		http.Error(w, chainErr.Error(), statusCode)
		return
	}
	eid := uuid.New()
	log.Errorf("%s [eid=%s]: %+v", msg, eid, err)
	http.Error(w, fmt.Sprintf("%s [eid: %s]", msg, eid), statusCode)
}

// PDPRoutePath is the base path for PDP routes
const PDPRoutePath = "/pdp"

const (
	// MaxCreateDataSetExtraDataSize defines the limit for extraData size in CreateDataSet calls (4KB).
	MaxCreateDataSetExtraDataSize = 4096

	// MaxAddPiecesBatchSize caps pieces per AddPieces (or CreateDataSetAndAddPieces)
	// call to reject early rather than revert on-chain.
	MaxAddPiecesBatchSize = 40

	// MaxDeletePieceExtraDataSize defines the limit for extraData size in DeletePiece calls (256B).
	MaxDeletePieceExtraDataSize = 256

	MaxDeletePiecesBatchSize = 500
)

// PDPService represents the service for managing data sets and pieces
type PDPService struct {
	Auth
	db      *harmonydb.DB
	storage paths.StashStore

	sender    *message.SenderETH
	ethClient ethchain.EthClient
	filClient PDPServiceNodeApi

	alertTask *alertmanager.AlertTask

	pullHandler *PullHandler

	ipp *ipni_provider.Provider

	ipOffenseThrottle *IPOffenseThrottle
}

type PDPServiceNodeApi interface {
	ChainHead(ctx context.Context) (*types2.TipSet, error)
}

// NewPDPService creates a new instance of PDPService with the provided stores.
func NewPDPService(
	ctx context.Context,
	db *harmonydb.DB,
	stor paths.StashStore,
	ec ethchain.EthClient,
	fc PDPServiceNodeApi,
	sn *message.SenderETH,
	alertTask *alertmanager.AlertTask,
	ipp *ipni_provider.Provider) *PDPService {
	auth := &NullAuth{}
	pullStore := NewDBPullStore(db)
	pullValidator := NewEthCallValidator(ec, db)

	p := &PDPService{
		Auth:    auth,
		db:      db,
		storage: stor,

		sender:    sn,
		ethClient: ec,
		filClient: fc,

		alertTask: alertTask,

		pullHandler: NewPullHandler(auth, pullStore, pullValidator, db),

		ipp: ipp,

		ipOffenseThrottle: NewIPOffenseThrottle(defaultIPOffensePolicies()),
	}

	go p.ipOffenseThrottle.RunCleanup(ctx)
	go p.cleanup(ctx)
	return p
}

// kvDataSetID extracts the dataset URL param for lifecycle logging.
func kvDataSetID(r *http.Request) []any {
	return []any{"dataset", chi.URLParam(r, "dataSetId")}
}

// kvDataSetPiece extracts dataset + piece URL params for lifecycle logging.
func kvDataSetPiece(r *http.Request) []any {
	return []any{"dataset", chi.URLParam(r, "dataSetId"), "piece_id", chi.URLParam(r, "pieceID")}
}

// kvUploadUUID extracts the upload UUID URL param for lifecycle logging.
func kvUploadUUID(r *http.Request) []any {
	return []any{"upload_uuid", chi.URLParam(r, "uploadUUID")}
}

// Routes registers the HTTP routes with the provided router.
func Routes(r chi.Router, p *PDPService) {
	mountExploreRoutes(r, p)

	r.Route(PDPRoutePath, func(r chi.Router) {
		r.Use(p.ipOffenseThrottle.Middleware)

		// Routes for data sets
		r.Route("/data-sets", func(r chi.Router) {
			// POST /pdp/data-sets - Create a new data set
			r.Post("/", instrument("dataSetCreate", p.handleCreateDataSet, nil))

			// POST /pdp/data-sets/create-and-add - Create a new data set and add pieces at the same time
			r.Post("/create-and-add", instrument("dataSetCreateAndAdd", p.handleCreateDataSetAndAddPieces, nil))

			// GET /pdp/data-sets/created/{txHash} - Get the status of a data set creation
			r.Get("/created/{txHash}", p.handleGetDataSetCreationStatus)

			// Individual data set routes
			r.Route("/{dataSetId}", func(r chi.Router) {
				// GET /pdp/data-sets/{set-id}
				r.Get("/", p.handleGetDataSet)

				// POST /pdp/data-sets/{set-id}/terminate
				r.Post("/terminate", instrument("dataSetTerminate", p.handleTerminateDataSet, kvDataSetID))

				// GET /pdp/data-sets/{set-id}/terminate
				r.Get("/terminate", p.handleGetDataSetTerminationStatus)

				// Routes for pieces within a data set
				r.Route("/pieces", func(r chi.Router) {
					// POST /pdp/data-sets/{set-id}/pieces
					r.Post("/", instrument("pieceAdd", p.handleAddPieceToDataSet, kvDataSetID))

					// GET /pdp/data-sets/{set-id}/pieces/added/{txHash}
					r.Get("/added/{txHash}", p.handleGetPieceAdditionStatus)

					// Individual piece routes
					r.Route("/{pieceID}", func(r chi.Router) {
						// GET /pdp/data-sets/{set-id}/pieces/{piece-id}
						r.Get("/", p.handleGetDataSetPiece)

						// DEL /pdp/data-sets/{set-id}/pieces/{piece-id}
						r.Delete("/", instrument("pieceDelete", p.handleDeleteDataSetPiece, kvDataSetPiece))
					})
				})
			})
		})

		r.Get("/ping", p.handlePing)

		// GET /pdp/piece/{pieceCid}/status - Get indexing/IPNI status for a piece
		r.Get("/piece/{pieceCid}/status", p.handleGetPieceStatus)

		// Routes for piece storage and retrieval
		// POST /pdp/piece
		r.Post("/piece", instrument("pieceUploadInit", p.handlePiecePost, nil))

		// GET /pdp/piece
		r.Get("/piece", p.handleFindPiece)

		// PUT /pdp/piece/upload/{uploadUUID}
		r.Put("/piece/upload/{uploadUUID}", instrument("pieceUpload", p.handlePieceUpload, kvUploadUUID))

		// POST /pdp/piece/uploads
		r.Post("/piece/uploads", instrument("pieceStreamInit", p.handleStreamingUploadURL, nil))

		// PUT /pdp/piece/uploads/{uploadUUID}
		r.Put("/piece/uploads/{uploadUUID}", instrument("pieceStreamUpload", p.handleStreamingUpload, kvUploadUUID))

		// POST /pdp/piece/uploads/{uploadUUID}
		r.Post("/piece/uploads/{uploadUUID}", instrument("pieceStreamFinalize", p.handleFinalizeStreamingUpload, kvUploadUUID))

		// POST /pdp/piece/pull - Pull pieces from other SPs
		r.Post("/piece/pull", instrument("piecePull", p.pullHandler.HandlePull, nil))
	})
}

// Handler functions

func (p *PDPService) handlePing(w http.ResponseWriter, r *http.Request) {
	_, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Failed to authorize request", err)
		return
	}

	if p.alertTask != nil && p.alertTask.Problems() {
		httpServerError(w, http.StatusServiceUnavailable, "Service Unavailable", nil)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleGetPieceStatus returns the indexing and IPNI status for a piece
func (p *PDPService) handleGetPieceStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify authorization
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Failed to authorize request", err)
		return
	}

	// Extract pieceCid from URL and convert to v1 for DB query
	pieceCidStr := chi.URLParam(r, "pieceCid")
	if pieceCidStr == "" {
		http.Error(w, "Missing pieceCid in URL", http.StatusBadRequest)
		return
	}

	// Convert to v1 format (database stores v1)
	info, err := ParsePieceCid(pieceCidStr)
	if err != nil {
		http.Error(w, "Invalid pieceCid format: "+err.Error(), http.StatusBadRequest)
		return
	}
	pieceCidV1Str := info.CidV1.String()

	// Query status from database
	var results []struct {
		PieceCID                 string         `db:"piece_cid"`
		PieceRawSize             uint64         `db:"piece_raw_size"`
		CreatedAt                time.Time      `db:"created_at"`
		Indexed                  bool           `db:"indexed"`
		IndexedAt                sql.NullTime   `db:"indexed_at"`
		AdvertisementCreated     bool           `db:"advertisement_created"`
		AdvertisementCreatedAt   sql.NullTime   `db:"advertisement_created_at"`
		AdCID                    sql.NullString `db:"ad_cid"`
		AdvertisementRetrieved   bool           `db:"advertisement_retrieved"`
		AdvertisementRetrievedAt sql.NullTime   `db:"advertisement_retrieved_at"`
		Status                   string         `db:"status"`
		Provider                 sql.NullString `db:"provider"`
	}

	err = p.db.Select(ctx, &results, `
		SELECT
			pr.piece_cid,
			pp.piece_raw_size,
			pr.created_at,

			-- Indexing status (true when CAR indexing completed and ready for/in IPNI)
			(pr.needs_ipni OR pr.ipni_task_id IS NOT NULL OR ia.ad_cid IS NOT NULL) as indexed,
			pr.indexed_at,

			-- Advertisement status
			ia.ad_cid IS NOT NULL as advertisement_created,
			pr.advertisement_created_at as advertisement_created_at,
			ia.ad_cid,

			-- Advertisement Fetch status
			ia.fetched_at IS NOT NULL as advertisement_retrieved,
			ia.fetched_at as advertisement_retrieved_at,

			-- Determine overall status
			CASE
				WHEN ia.fetched_at IS NOT NULL THEN 'retrieved'
				WHEN ia.ad_cid IS NOT NULL THEN 'announced'
				WHEN pr.ipni_task_id IS NOT NULL THEN 'creating_ad'
				WHEN pr.indexing_task_id IS NOT NULL THEN 'indexing'
				ELSE 'pending'
			END as status,
		
			ia.provider

		FROM pdp_piecerefs pr
		JOIN parked_piece_refs pprf ON pprf.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		LEFT JOIN LATERAL (
			SELECT
				MIN(i.ad_cid) as ad_cid,
				MIN(i.provider) as provider,
				MIN((SELECT MIN(af.fetched_at) FROM ipni_ad_fetches af WHERE af.ad_cid = i.ad_cid)) as fetched_at
			FROM ipni i
			WHERE i.piece_cid = pr.piece_cid
				AND i.provider = (SELECT peer_id FROM ipni_peerid WHERE sp_id = $3)
				AND i.is_rm = FALSE
		) ia ON true
		WHERE pr.piece_cid = $1 AND pr.service = $2
		LIMIT 1
	`, pieceCidV1Str, serviceLabel, indexing.PDP_v0_SP_ID)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to query piece status", err)
		return
	}

	if len(results) == 0 {
		http.Error(w, "Piece not found or does not belong to service", http.StatusNotFound)
		return
	}

	result := results[0]

	// Convert authoritative PieceCID back from v1 to v2 for external API
	pieceInfo, err := PieceCidV2FromV1Str(result.PieceCID, result.PieceRawSize)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to convert PieceCID to v2", err)
		return
	}

	// Prepare response
	response := struct {
		PieceCID     string     `json:"pieceCid"`
		Status       string     `json:"status"`
		Indexed      bool       `json:"indexed"`
		IndexedAt    *time.Time `json:"indexedAt,omitempty"`
		AdCreated    bool       `json:"adCreated"`
		AdCreatedAt  *time.Time `json:"adCreatedAt,omitempty"`
		Advertised   bool       `json:"advertised"`
		AdvertisedAt *time.Time `json:"advertisedAt,omitempty"`
		Retrieved    bool       `json:"retrieved"`
		RetrievedAt  *time.Time `json:"retrievedAt,omitempty"`
	}{
		PieceCID:  pieceInfo.CidV2.String(),
		Status:    result.Status,
		Indexed:   result.Indexed,
		AdCreated: result.AdvertisementCreated,
		Retrieved: result.AdvertisementRetrieved,
	}

	if !result.IndexedAt.Valid {
		response.IndexedAt = nil
	} else {
		response.IndexedAt = &result.IndexedAt.Time
	}

	if !result.AdvertisementCreatedAt.Valid {
		response.AdCreatedAt = nil
	} else {
		response.AdCreatedAt = &result.AdvertisementCreatedAt.Time
	}

	if !result.AdvertisementRetrievedAt.Valid {
		response.RetrievedAt = nil
	} else {
		response.RetrievedAt = &result.AdvertisementRetrievedAt.Time
	}

	// Advertised and AdvertisedAt are derived from three signals, in order:
	// 1. A recorded fetch of this ad in ipni_ad_fetches is the strongest per-ad
	//    signal. If an indexer fetched the ad, it must have been advertised
	//    already. Since we do not store the actual first publish time for each
	//    ad, AdvertisedAt is estimated as the earlier of
	//    advertisement_created_at + PublishInterval and the first fetch time.
	//    This keeps AdvertisedAt from appearing after RetrievedAt.
	// 2. The in-process IPNI provider exposes LastPublishTime per provider, not
	//    per ad. It is useful only when there is no fetch record and we know
	//    when this ad was created. A provider publish after this ad was created
	//    means the ad should have been included in the announced head; an older
	//    provider publish means it was not, and we do not fall through to the
	//    timing heuristic.
	// 3. Without either signal, fall back to the old timing heuristic: after
	//    PublishInterval has elapsed from ad creation, assume the ad was
	//    announced.
	if result.AdvertisementRetrieved {
		response.Advertised = true
		if result.AdvertisementRetrievedAt.Valid {
			advertisedAt := result.AdvertisementRetrievedAt.Time
			if result.AdvertisementCreatedAt.Valid {
				createdAtEstimate := result.AdvertisementCreatedAt.Time.Add(ipni_provider.PublishInterval)
				if createdAtEstimate.Before(advertisedAt) {
					advertisedAt = createdAtEstimate
				}
			}
			response.AdvertisedAt = &advertisedAt
		} else {
			response.AdvertisedAt = nil
		}
	}

	advertisedFromProvider := false
	if !response.Advertised && result.AdvertisementCreatedAt.Valid && p.ipp != nil && result.Provider.Valid {
		publishedAt := p.ipp.LastPublishTime(result.Provider.String)
		if publishedAt != nil {
			advertisedFromProvider = true
			if publishedAt.After(result.AdvertisementCreatedAt.Time) {
				response.Advertised = true
				response.AdvertisedAt = new(*publishedAt)
				if publishedAt.After(time.Now().Add(ipni_provider.PublishInterval)) {
					response.AdvertisedAt = new(result.AdvertisementCreatedAt.Time.Add(ipni_provider.PublishInterval))
				}
			} else {
				response.Advertised = false
				response.AdvertisedAt = nil
			}
		}
	}

	if !advertisedFromProvider && !response.Advertised {
		if result.AdvertisementCreated && result.AdvertisementCreatedAt.Valid {
			if time.Since(result.AdvertisementCreatedAt.Time) > ipni_provider.PublishInterval {
				// More than 5 seconds since advertisement was created, assume it's published
				response.Advertised = true
				response.AdvertisedAt = new(result.AdvertisementCreatedAt.Time.Add(ipni_provider.PublishInterval))
			} else {
				response.Advertised = false
				response.AdvertisedAt = nil
			}
		}
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// getSenderAddress retrieves the sender address from the database where role = 'pdp' limit 1
func (p *PDPService) getSenderAddress(ctx context.Context) (common.Address, error) {
	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
}

// handleGetDataSetCreationStatus handles the GET request to retrieve the status of a data set creation
func (p *PDPService) handleGetDataSetCreationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract txHash from the URL
	txHash := chi.URLParam(r, "txHash")
	if txHash == "" {
		http.Error(w, "Missing txHash in URL", http.StatusBadRequest)
		return
	}

	// Clean txHash (ensure it starts with '0x' and is lowercase)
	if !strings.HasPrefix(txHash, "0x") {
		txHash = "0x" + txHash
	}
	txHash = strings.ToLower(txHash)

	log.Debugw("GetDataSetCreationStatus request",
		"txHash", txHash,
		"service", serviceLabel)

	// Validate txHash is a valid hash
	if len(txHash) != 66 { // '0x' + 64 hex chars
		http.Error(w, "Invalid txHash length", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(txHash[2:]); err != nil {
		http.Error(w, "Invalid txHash format", http.StatusBadRequest)
		return
	}

	// Step 3: Lookup pdp_data_set_creates by create_message_hash (which is txHash)
	var dataSetCreate struct {
		CreateMessageHash string `db:"create_message_hash"`
		OK                *bool  `db:"ok"` // Pointer to handle NULL
		DataSetCreated    bool   `db:"data_set_created"`
		Service           string `db:"service"`
	}

	err = p.db.QueryRow(ctx, `
        SELECT create_message_hash, ok, data_set_created, service
        FROM pdp_data_set_creates
        WHERE create_message_hash = $1
    `, txHash).Scan(&dataSetCreate.CreateMessageHash, &dataSetCreate.OK, &dataSetCreate.DataSetCreated, &dataSetCreate.Service)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set creation not found for given txHash", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to query data set creation", err)
		return
	}

	// Step 4: Check that the service matches the requesting service
	if dataSetCreate.Service != serviceLabel {
		http.Error(w, "Unauthorized: service label mismatch", http.StatusUnauthorized)
		return
	}

	// Step 5: Prepare the response
	response := struct {
		CreateMessageHash string  `json:"createMessageHash"`
		DataSetCreated    bool    `json:"dataSetCreated"`
		Service           string  `json:"service"`
		TxStatus          string  `json:"txStatus"`
		OK                *bool   `json:"ok"`
		DataSetId         *uint64 `json:"dataSetId,omitempty"`
	}{
		CreateMessageHash: dataSetCreate.CreateMessageHash,
		DataSetCreated:    dataSetCreate.DataSetCreated,
		Service:           dataSetCreate.Service,
		OK:                dataSetCreate.OK,
	}

	// Now get the tx_status from message_waits_eth
	var txStatus string
	err = p.db.QueryRow(ctx, `
        SELECT tx_status
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, txHash).Scan(&txStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// This should not happen as per foreign key constraints
			http.Error(w, "Message status not found for given txHash", http.StatusInternalServerError)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to query message status", err)
		return
	}

	response.TxStatus = txStatus

	if dataSetCreate.DataSetCreated {
		// The data set has been created, get the dataSetId from pdp_data_sets
		var dataSetId uint64
		err = p.db.QueryRow(ctx, `
            SELECT id
            FROM pdp_data_sets
            WHERE create_message_hash = $1
        `, txHash).Scan(&dataSetId)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Should not happen, but handle gracefully
				http.Error(w, "Data set not found despite data_set_created = true", http.StatusInternalServerError)
				return
			}
			httpServerError(w, http.StatusInternalServerError, "Failed to query data set", err)
			return
		}
		response.DataSetId = &dataSetId
	}

	log.Debugw("GetDataSetCreationStatus response",
		"txHash", txHash,
		"txStatus", response.TxStatus,
		"dataSetCreated", response.DataSetCreated,
		"ok", response.OK,
		"dataSetId", response.DataSetId)

	// Step 6: Return the response as JSON
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleGetDataSet handles the GET request to retrieve the details of a data set
func (p *PDPService) handleGetDataSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract dataSetId from the URL
	dataSetIdStr := chi.URLParam(r, "dataSetId")
	if dataSetIdStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}

	// Convert dataSetId to uint64
	dataSetId, err := strconv.ParseUint(dataSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	// Step 3: Retrieve the data set from the database
	var dataSet struct {
		ID      uint64 `db:"id"`
		Service string `db:"service"`
	}

	err = p.db.QueryRow(ctx, `
        SELECT id, service
        FROM pdp_data_sets
        WHERE id = $1
    `, dataSetId).Scan(&dataSet.ID, &dataSet.Service)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set", err)
		return
	}

	// Step 4: Check that the data set belongs to the requesting service
	if dataSet.Service != serviceLabel {
		http.Error(w, "Unauthorized: data set does not belong to your service", http.StatusUnauthorized)
		return
	}

	// Step 5: Retrieve the pieces associated with the data set
	// Join with parked_pieces to get the raw size for sub-pieces
	// Note: aggregate pieces are not stored, only sub-pieces
	var pieces []struct {
		PieceID         uint64 `db:"piece_id"`
		PieceCid        string `db:"piece"`
		Removed         bool   `db:"removed"`
		SubPieceCID     string `db:"sub_piece"`
		SubPieceOffset  int64  `db:"sub_piece_offset"`
		SubPieceSize    int64  `db:"sub_piece_size"`
		SubPieceRawSize uint64 `db:"sub_piece_raw_size"`
	}

	err = p.db.Select(ctx, &pieces, `
        SELECT
            dsp.piece_id,
            dsp.piece,
            dsp.removed,
            dsp.sub_piece,
            dsp.sub_piece_offset,
            dsp.sub_piece_size,
            pp.piece_raw_size AS sub_piece_raw_size
        FROM pdp_data_set_pieces dsp
        -- Use pdp_pieceref to get to the sub-piece's raw size
        JOIN pdp_piecerefs ppr ON ppr.id = dsp.pdp_pieceref
        JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
        JOIN parked_pieces pp ON pp.id = pprf.piece_id
        WHERE dsp.data_set = $1
        ORDER BY dsp.piece_id, dsp.sub_piece_offset
    `, dataSetId)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set pieces", err)
		return
	}

	// Step 6: Get the next challenge epoch (can be NULL for uninitialized data sets)
	var nextChallengeEpoch *int64
	err = p.db.QueryRow(ctx, `
        SELECT prove_at_epoch
        FROM pdp_data_sets
        WHERE id = $1
    `, dataSetId).Scan(&nextChallengeEpoch)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve next challenge epoch", err)
		return
	}

	// Step 7: Prepare the response
	// Use 0 to indicate uninitialized data set (no challenge epoch set yet)
	// This maintains compatibility with SDK expectations
	epochValue := int64(0)
	if nextChallengeEpoch != nil {
		epochValue = *nextChallengeEpoch
	}

	response := struct {
		ID                 uint64       `json:"id"`
		Pieces             []PieceEntry `json:"pieces"`
		NextChallengeEpoch int64        `json:"nextChallengeEpoch"`
	}{
		ID:                 dataSet.ID,
		NextChallengeEpoch: epochValue,
		Pieces:             []PieceEntry{}, // Initialize as empty array, not nil
	}

	// Calculate aggregate piece raw sizes by summing sub-piece raw sizes (group by piece_id)
	pieceRawSizes := make(map[uint64]uint64)
	for _, piece := range pieces {
		pieceRawSizes[piece.PieceID] += piece.SubPieceRawSize
	}

	aggregatePieceCIDs := make(map[uint64]string)

	showRemoved := chi.URLParam(r, "XXXshowRemoved") == "true"
	// Convert pieces to the desired JSON format
	for _, piece := range pieces {
		if !showRemoved && piece.Removed {
			continue
		}

		// Calculate aggregate piece CID on first use for this piece_id
		pcv2Str, exists := aggregatePieceCIDs[piece.PieceID]
		if !exists {
			aggregateRawSize := pieceRawSizes[piece.PieceID]
			pcInfo, err := PieceCidV2FromV1Str(piece.PieceCid, aggregateRawSize)
			if err != nil {
				http.Error(w, "Invalid PieceCID: "+err.Error(), http.StatusBadRequest)
				return
			}
			pcv2Str = pcInfo.CidV2.String()
			aggregatePieceCIDs[piece.PieceID] = pcv2Str
		}

		// Use the raw size for the sub piece
		spcInfo, err := PieceCidV2FromV1Str(piece.SubPieceCID, piece.SubPieceRawSize)
		if err != nil {
			http.Error(w, "Invalid SubPieceCID: "+err.Error(), http.StatusBadRequest)
			return
		}
		response.Pieces = append(response.Pieces, PieceEntry{
			PieceID:        piece.PieceID,
			PieceCID:       pcv2Str,
			SubPieceCID:    spcInfo.CidV2.String(),
			SubPieceOffset: piece.SubPieceOffset,
		})
	}

	// Step 8: Return the response as JSON
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// PieceEntry represents a piece in the data set for JSON serialization
type PieceEntry struct {
	PieceID        uint64 `json:"pieceId"`
	PieceCID       string `json:"pieceCid"`
	SubPieceCID    string `json:"subPieceCid"`
	SubPieceOffset int64  `json:"subPieceOffset"`
}

// handleGetPieceAdditionStatus handles GET /pdp/data-sets/{dataSetId}/pieces/added/{txHash}
func (p *PDPService) handleGetPieceAdditionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract parameters from the URL
	dataSetIdStr := chi.URLParam(r, "dataSetId")
	txHash := chi.URLParam(r, "txHash")

	if dataSetIdStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}
	if txHash == "" {
		http.Error(w, "Missing transaction hash in URL", http.StatusBadRequest)
		return
	}

	// Convert dataSetId to uint64
	dataSetId, err := strconv.ParseUint(dataSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	// Clean txHash (ensure it starts with '0x' and is lowercase)
	if !strings.HasPrefix(txHash, "0x") {
		txHash = "0x" + txHash
	}
	txHash = strings.ToLower(txHash)

	// Validate txHash is a valid hash
	if len(txHash) != 66 { // '0x' + 64 hex chars
		http.Error(w, "Invalid txHash length", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(txHash[2:]); err != nil {
		http.Error(w, "Invalid txHash format", http.StatusBadRequest)
		return
	}

	// Step 3: Verify data set ownership
	var dataSetService string
	err = p.db.QueryRow(ctx, `
		SELECT service
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetId).Scan(&dataSetService)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set", err)
		return
	}

	if dataSetService != serviceLabel {
		// Same response as not found to avoid leaking information
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}

	// Step 4: Query pdp_data_set_piece_adds for this transaction
	type PieceAddInfo struct {
		Piece           string `db:"piece"`
		AddMessageIndex int    `db:"add_message_index"`
		SubPiece        string `db:"sub_piece"`
		SubPieceOffset  int64  `db:"sub_piece_offset"`
		SubPieceSize    int64  `db:"sub_piece_size"`
		AddMessageOK    *bool  `db:"add_message_ok"`
		PiecesAdded     bool   `db:"pieces_added"`
	}

	var pieceAdds []PieceAddInfo
	err = p.db.Select(ctx, &pieceAdds, `
		SELECT piece, add_message_index, sub_piece, sub_piece_offset,
		       sub_piece_size, add_message_ok, pieces_added
		FROM pdp_data_set_piece_adds
		WHERE data_set = $1 AND add_message_hash = $2
		ORDER BY add_message_index, sub_piece_offset
	`, dataSetId, txHash)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to query piece additions", err)
		return
	}

	if len(pieceAdds) == 0 {
		http.Error(w, "Piece addition not found for given transaction", http.StatusNotFound)
		return
	}

	// Step 5: Get transaction status from message_waits_eth
	var txStatus string
	err = p.db.QueryRow(ctx, `
		SELECT tx_status FROM message_waits_eth WHERE signed_tx_hash = $1
	`, txHash).Scan(&txStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Transaction status not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to query transaction status", err)
		return
	}

	// Determine unique pieces list
	uniquePieceMap := make(map[string]bool)
	for _, ra := range pieceAdds {
		uniquePieceMap[ra.Piece] = true
	}

	// Step 6: If transaction is confirmed and successful, get assigned piece IDs
	var confirmedPieceIds []uint64
	if txStatus == "confirmed" && len(pieceAdds) > 0 && pieceAdds[0].AddMessageOK != nil && *pieceAdds[0].AddMessageOK {
		// Query pdp_data_set_pieces directly using the transaction hash
		// This gives us the exact pieces added in THIS transaction even if there are duplicate pieces
		err = p.db.Select(ctx, &confirmedPieceIds, `
			SELECT DISTINCT piece_id
			FROM pdp_data_set_pieces
			WHERE data_set = $1
			  AND add_message_hash = $2
			ORDER BY piece_id
		`, dataSetId, txHash)
		if err != nil {
			httpServerError(w, http.StatusInternalServerError, "Failed to query confirmed pieces", err)
			return
		}
	}

	if confirmedPieceIds != nil && len(confirmedPieceIds) != len(pieceAdds) {
		msg := fmt.Sprintf("Mismatch in confirmed piece IDs count (%d) vs number of pieces added (%d) for tx %s", len(confirmedPieceIds), len(pieceAdds), txHash)
		log.Error(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	} // else confirmedPieceIds is nil because they haven't landed yet, or we got the right number of confirmed pieces

	// Step 7: Build and send response
	// Check that all pieces have the same PiecesAdded value (consistency check)
	if len(pieceAdds) > 0 {
		firstPiecesAdded := pieceAdds[0].PiecesAdded
		for _, pa := range pieceAdds[1:] {
			if pa.PiecesAdded != firstPiecesAdded {
				http.Error(w, "Inconsistent piecesAdded state for this transaction's pieces", http.StatusInternalServerError)
				return
			}
		}
	}
	allPiecesProcessed := false
	if len(pieceAdds) > 0 {
		allPiecesProcessed = pieceAdds[0].PiecesAdded
	}

	response := struct {
		TxHash            string   `json:"txHash"`
		TxStatus          string   `json:"txStatus"`
		DataSetId         uint64   `json:"dataSetId"`
		PieceCount        int      `json:"pieceCount"`
		AddMessageOK      *bool    `json:"addMessageOk"`
		PiecesAdded       bool     `json:"piecesAdded"`
		ConfirmedPieceIds []uint64 `json:"confirmedPieceIds,omitempty"`
	}{
		TxHash:            txHash,
		TxStatus:          txStatus,
		DataSetId:         dataSetId,
		PieceCount:        len(uniquePieceMap),
		AddMessageOK:      pieceAdds[0].AddMessageOK,
		PiecesAdded:       allPiecesProcessed,
		ConfirmedPieceIds: confirmedPieceIds,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func normalizeDeletePieceIDs(ids []uint64) ([]int64, error) {
	if len(ids) > MaxDeletePiecesBatchSize {
		return nil, fmt.Errorf("piece count (%d) exceeds the maximum allowed per DeletePiece call (%d)", len(ids), MaxDeletePiecesBatchSize)
	}
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > math.MaxInt64 {
			return nil, fmt.Errorf("piece ID %d is out of range", id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, int64(id))
	}
	return out, nil
}

func (p *PDPService) handleDeleteDataSetPiece(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract parameters from the URL
	dataSetIdStr := chi.URLParam(r, "dataSetId")
	if dataSetIdStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}
	pieceIdStr := chi.URLParam(r, "pieceID")
	if pieceIdStr == "" {
		http.Error(w, "Missing piece ID in URL", http.StatusBadRequest)
		return
	}

	// Convert dataSetId to uint64
	dataSetId, err := strconv.ParseUint(dataSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}
	pieceID, err := strconv.ParseUint(pieceIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid piece ID format", http.StatusBadRequest)
		return
	}

	// check if the data set belongs to the service in pdp_data_sets
	var dataSetService string
	err = p.db.QueryRow(ctx, `
			SELECT service
			FROM pdp_data_sets
			WHERE id = $1
		`, dataSetId).Scan(&dataSetService)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set", err)
		return
	}

	if dataSetService != serviceLabel {
		// same as when actually not found to avoid leaking information in obvious ways
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}
	type DeletePiecePayload struct {
		ExtraData *string  `json:"extraData"`
		PieceIDs  []uint64 `json:"pieceIds"`
	}
	var payload DeletePiecePayload
	err = json.NewDecoder(r.Body).Decode(&payload)

	// if the request body is empty, json.Decode will return io.EOF
	if err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var extraDataBytes []byte
	if payload.ExtraData != nil {
		extraDataHexStr := *payload.ExtraData
		extraDataBytes, err = hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
		if err != nil {
			log.Errorf("Failed to decode hex extraData: %v", err)
			http.Error(w, "Invalid extraData format (must be hex encoded)", http.StatusBadRequest)
			return
		}
		if len(extraDataBytes) > MaxDeletePieceExtraDataSize {
			errMsg := fmt.Sprintf("extraData size (%d bytes) exceeds the maximum allowed limit for DeletePiece (%d bytes)", len(extraDataBytes), MaxDeletePieceExtraDataSize)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	}

	pieceIDs := []uint64{pieceID}
	if len(payload.PieceIDs) > 0 {
		pieceIDs = payload.PieceIDs
	}

	pieceIDsI64, err := normalizeDeletePieceIDs(pieceIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var foundCount int
	err = p.db.QueryRow(ctx, `SELECT COUNT(DISTINCT piece_id) FROM pdp_data_set_pieces WHERE data_set = $1 AND piece_id = ANY($2::bigint[])`, dataSetId, pieceIDsI64).Scan(&foundCount)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to query piece existence", err)
		return
	}
	if foundCount != len(pieceIDsI64) {
		http.Error(w, "One or more piece not found", http.StatusNotFound)
		return
	}

	// Get the ABI and pack the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pieceIDArgs := make([]*big.Int, len(pieceIDsI64))
	for i, id := range pieceIDsI64 {
		pieceIDArgs[i] = big.NewInt(id)
	}

	// Pack the method call data
	data, err := abiData.Pack("schedulePieceDeletions",
		big.NewInt(int64(dataSetId)),
		pieceIDArgs,
		[]byte(extraDataBytes),
	)
	if err != nil {
		http.Error(w, "Failed to pack method call: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the sender address
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to get sender address", err)
		return
	}

	// Prepare the transaction
	ethTx := types.NewTransaction(
		0, // nonce will be set by SenderETH
		contract.ContractAddresses().PDPVerifier,
		big.NewInt(0), // value
		0,             // gas limit (will be estimated)
		nil,           // gas price (will be set by SenderETH)
		data,
	)

	// Send the transaction
	reason := "pdp-delete-piece"
	txHash, err := p.sender.Send(ctx, fromAddress, ethTx, reason)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to send transaction", err)
		return
	}

	// Schedule deletion of the piece from the data set using a transaction
	txHashLower := strings.ToLower(txHash.Hex())
	log.Infow("PDP DeletePiece: Creating transaction tracking record", "txHash", txHashLower)

	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err := tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, $2)
		`, txHashLower, "pending")
		if err != nil {
			log.Errorw("Failed to insert into message_waits_eth",
				"txHash", txHashLower,
				"error", err)
			return false, err
		}

		_, err = tx.Exec(`
			UPDATE pdp_data_set_pieces
			SET rm_message_hash = $1
			WHERE data_set = $2 AND piece_id = ANY($3::bigint[])`,
			txHashLower, dataSetId, pieceIDsI64)
		if err != nil {
			log.Errorw("Failed to update rm_message_hash in pdp_data_set_pieces", "dataSetId", dataSetId, "pieceIDs", pieceIDsI64, "error", err)
			return false, err
		}
		log.Infow("scheduled user requested deletion", "dataSetId", dataSetId, "pieceIDs", pieceIDsI64, "txHash", txHashLower)

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to schedule delete piece", err)
		return
	}

	response := struct {
		TxHash string `json:"txHash"`
	}{
		TxHash: txHashLower,
	}
	// Send JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to encode response", err)
		return
	}
}

func (p *PDPService) handleGetDataSetPiece(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Failed to authorize request", err)
		return
	}

	// Step 2: Extract and validate parameters
	dataSetIdStr := chi.URLParam(r, "dataSetId")
	pieceIDStr := chi.URLParam(r, "pieceID")

	if dataSetIdStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}
	if pieceIDStr == "" {
		http.Error(w, "Missing piece ID in URL", http.StatusBadRequest)
		return
	}

	dataSetId, err := strconv.ParseUint(dataSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	pieceID, err := strconv.ParseUint(pieceIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid piece ID format", http.StatusBadRequest)
		return
	}

	// Step 3: Verify ownership and get piece details
	var pieceCid string
	err = p.db.QueryRow(ctx, `
		SELECT DISTINCT r.piece
		FROM pdp_data_set_pieces r
		JOIN pdp_data_sets ps ON ps.id = r.data_set
		WHERE r.data_set = $1 AND r.piece_id = $2 AND ps.service = $3
	`, dataSetId, pieceID, serviceLabel).Scan(&pieceCid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Piece not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve piece", err)
		return
	}

	// Step 4: Get all subPieces for this piece
	type SubPieceInfo struct {
		SubPieceCID    string `db:"sub_piece"`
		SubPieceOffset int64  `db:"sub_piece_offset"`
	}

	var subPieces []SubPieceInfo
	err = p.db.Select(ctx, &subPieces, `
		SELECT sub_piece, sub_piece_offset
		FROM pdp_data_set_pieces
		WHERE data_set = $1 AND piece_id = $2
		ORDER BY sub_piece_offset
	`, dataSetId, pieceID)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve subPieces", err)
		return
	}

	// Step 5: Build response according to spec
	type SubPieceResponse struct {
		SubPieceCid    string `json:"subPieceCid"`
		SubPieceOffset int64  `json:"subPieceOffset"`
	}

	response := struct {
		PieceId   uint64             `json:"pieceId"`
		PieceCID  string             `json:"pieceCid"`
		SubPieces []SubPieceResponse `json:"subPieces"`
	}{
		PieceId:   pieceID,
		PieceCID:  pieceCid,
		SubPieces: make([]SubPieceResponse, 0, len(subPieces)),
	}

	for _, subPiece := range subPieces {
		response.SubPieces = append(response.SubPieces, SubPieceResponse{
			SubPieceCid:    subPiece.SubPieceCID,
			SubPieceOffset: subPiece.SubPieceOffset,
		})
	}

	// Step 6: Send JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to encode response", err)
		return
	}
}

func (p *PDPService) cleanup(ctx context.Context) {
	rm := func(ctx context.Context, db *harmonydb.DB) {
		var RefIDs []int64

		err := db.QueryRow(ctx, `SELECT COALESCE(array_agg(piece_ref), '{}') AS ref_ids
												FROM pdp_piece_streaming_uploads
												WHERE complete = TRUE
												  AND completed_at <= TIMEZONE('UTC', NOW()) - INTERVAL '60 minutes';`).Scan(&RefIDs)
		if err != nil {
			log.Errorw("failed to get non-finalized uploads", "error", err)
		}

		if len(RefIDs) > 0 {
			_, err := db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = ANY($1);`, RefIDs)
			if err != nil {
				log.Errorw("failed to delete non-finalized uploads", "error", err)
			}
		}

		// Clean up old piece pull records (older than 5 days). Pull items only
		// store refs created by PullPiece, so delete unused refs first;
		// otherwise the CASCADE from pdp_piece_pulls removes the pointer to
		// those refs.
		_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			_, err = tx.Exec(`
				WITH old_pull_refs AS (
					SELECT DISTINCT fi.parked_piece_ref AS ref_id
					FROM pdp_piece_pull_items fi
					JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
					WHERE pp.created_at < NOW() - INTERVAL '5 days'
						AND fi.parked_piece_ref IS NOT NULL
				)
				DELETE FROM parked_piece_refs pr
				USING old_pull_refs old
				WHERE pr.ref_id = old.ref_id
					AND NOT EXISTS (
						SELECT 1 FROM pdp_piecerefs ppr WHERE ppr.piece_ref = pr.ref_id
					)
					AND NOT EXISTS (
						SELECT 1
						FROM pdp_piece_pull_items live_fi
						JOIN pdp_piece_pulls live_pp ON live_pp.id = live_fi.fetch_id
						WHERE live_fi.parked_piece_ref = pr.ref_id
							AND live_pp.created_at >= NOW() - INTERVAL '5 days'
					)
				`)
			if err != nil {
				return false, fmt.Errorf("delete old pull refs: %w", err)
			}

			_, err = tx.Exec(`DELETE FROM pdp_piece_pulls WHERE created_at < NOW() - INTERVAL '5 days'`)
			if err != nil {
				return false, fmt.Errorf("delete old pull records: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			log.Errorw("failed to delete old piece pull records", "error", err)
		}
	}

	ticker := time.NewTicker(time.Minute * 5)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rm(ctx, p.db)
		case <-ctx.Done():
			return
		}
	}
}
