package pdp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"math/bits"
	"net/http"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// FetchRecord represents a fetch request record from the database
type FetchRecord struct {
	ID            int64
	Service       string
	ExtraDataHash []byte
	DataSetId     uint64 // 0 = create new
	RecordKeeper  string // address, required when DataSetId is 0
}

// PieceStatus represents the status of a piece in storage
type PieceStatus struct {
	PieceCid   string
	Complete   bool
	TaskID     *int64 // task_id from parked_pieces (may point to deleted task)
	TaskExists bool   // true if task_id exists in harmony_task
	Retries    int    // retry count from harmony_task (0 if task doesn't exist)
}

// ParkedPieceEntry represents the data needed to create a parked piece entry
type ParkedPieceEntry struct {
	PieceCid        string
	PiecePaddedSize int64
	PieceRawSize    int64
	DataURL         string
}

// FetchPiece represents a piece stored in a fetch request (v1 CID + raw size for v2 reconstruction)
type FetchPiece struct {
	CidV1      cid.Cid
	RawSize    uint64
	Failed     bool   // true if piece permanently failed
	FailReason string // error message when failed
}

// FetchStore abstracts database operations for the fetch handler
type FetchStore interface {
	// GetFetchByKey retrieves a fetch record by its idempotency key
	GetFetchByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*FetchRecord, error)

	// CreateFetchWithPieces creates a fetch record and its associated piece items in a transaction
	// Returns the created fetch ID
	CreateFetchWithPieces(ctx context.Context, fetch *FetchRecord, pieces []FetchPiece) (int64, error)

	// GetPieceStatuses retrieves the status of multiple pieces (keyed by v1 CID string)
	// Includes task retry count from harmony_task when available
	GetPieceStatuses(ctx context.Context, pieceCids []cid.Cid) (map[string]*PieceStatus, error)

	// CreateParkedPieceIfNotExists creates a parked piece and ref entry if the piece doesn't exist
	// Returns true if a new entry was created, false if it already existed
	CreateParkedPieceIfNotExists(ctx context.Context, entry *ParkedPieceEntry) (bool, error)

	// GetFetchPieces retrieves all pieces associated with a fetch record (includes failure info)
	GetFetchPieces(ctx context.Context, fetchID int64) ([]FetchPiece, error)

	// MarkPieceFailed marks a piece as permanently failed in the fetch items table
	MarkPieceFailed(ctx context.Context, fetchID int64, pieceCid string, reason string) error

	// CheckTaskExhaustedRetries checks if a task exhausted its retries by looking at harmony_task_history
	// Returns true if the task failed permanently, along with the error message
	CheckTaskExhaustedRetries(ctx context.Context, taskID int64) (bool, string, error)
}

// AddPiecesValidatorParams contains parameters for eth_call validation
type AddPiecesValidatorParams struct {
	DataSetId    *big.Int       // 0 for create-new
	RecordKeeper common.Address // only used when DataSetId is 0
	PieceData    []contract.CidsCid
	ExtraData    []byte
}

// AddPiecesValidator validates extraData against the contract via eth_call
type AddPiecesValidator interface {
	// ValidateAddPieces performs an eth_call to validate the extraData
	// Returns nil if validation passes, error otherwise
	ValidateAddPieces(ctx context.Context, params *AddPiecesValidatorParams) error
}

// EthCallValidator validates via eth_call to PDPVerifier contract
type EthCallValidator struct {
	ethClient *ethclient.Client
}

// NewEthCallValidator creates a validator that uses eth_call
func NewEthCallValidator(ethClient *ethclient.Client) *EthCallValidator {
	return &EthCallValidator{ethClient: ethClient}
}

func (v *EthCallValidator) ValidateAddPieces(ctx context.Context, params *AddPiecesValidatorParams) error {
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("failed to get contract ABI: %w", err)
	}

	// Build addPieces calldata
	// When dataSetId is 0 (create new), use recordKeeper address and sybil fee
	// When dataSetId > 0 (add to existing), use zero address and no fee
	isCreateNew := params.DataSetId.Cmp(big.NewInt(0)) == 0
	listenerAddr := common.Address{}
	if isCreateNew {
		listenerAddr = params.RecordKeeper
	}

	data, err := abiData.Pack("addPieces", params.DataSetId, listenerAddr, params.PieceData, params.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to pack addPieces call: %w", err)
	}

	// Perform eth_call - sybil fee only required for create-new case
	contractAddr := contract.ContractAddresses().PDPVerifier
	value := big.NewInt(0)
	if isCreateNew {
		value = contract.SybilFee()
	}
	msg := ethereum.CallMsg{
		To:    &contractAddr,
		Data:  data,
		Value: value,
	}

	_, err = v.ethClient.CallContract(ctx, msg, nil)
	if err != nil {
		return fmt.Errorf("addPieces validation failed: %w", err)
	}

	return nil
}

// FetchHandler handles piece fetch requests
type FetchHandler struct {
	auth      Auth
	store     FetchStore
	validator AddPiecesValidator
}

// NewFetchHandler creates a new FetchHandler
func NewFetchHandler(auth Auth, store FetchStore, validator AddPiecesValidator) *FetchHandler {
	return &FetchHandler{
		auth:      auth,
		store:     store,
		validator: validator,
	}
}

// HandleFetch handles POST /pdp/piece/fetch requests for SP-to-SP piece transfer.
//
// # Overview
//
// This endpoint allows a client to request that pieces be fetched from other storage
// providers and stored locally. It is designed for scenarios where data already exists
// on one SP and needs to be replicated to another, without requiring the client to
// re-upload the data.
//
// # Request Format
//
// The request body is a JSON object with the following fields:
//
//   - extraData (required): Hex-encoded bytes that will be validated against the
//     PDPVerifier contract via eth_call. This ensures the caller has authorization
//     to add these pieces. See "ExtraData and Authorization" below.
//
//   - dataSetId (optional): The target dataset ID for the eth_call validation.
//     If omitted or zero, validation simulates creating a new dataset.
//
//   - recordKeeper (required when creating new dataset): The contract address that
//     will receive callbacks from PDPVerifier (typically FilecoinWarmStorageService).
//     Must be in the allowed list for public services.
//
//   - pieces (required): Array of pieces to fetch, each containing:
//
//   - pieceCid: PieceCIDv2 format (encodes both CommP and raw size)
//
//   - sourceUrl: HTTPS URL ending in /piece/{pieceCid} on a public host
//
// # ExtraData and Authorization
//
// The extraData field serves two purposes:
//
//  1. Authorization: It is validated via eth_call to PDPVerifier.addPieces(), which
//     forwards to the recordKeeper contract for validation. PDPVerifier itself only
//     checks for valid input format; the recordKeeper (e.g., FilecoinWarmStorageService)
//     performs the actual authorization checks such as signature verification and
//     ensuring sufficient funds are available.
//
//  2. Idempotency key: Combined with service, dataSetId, and recordKeeper, a hash of
//     extraData forms the idempotency key. Repeated requests with the same key return
//     the status of the existing fetch rather than creating duplicates.
//
// The extraData used here does NOT need to match the extraData used in the subsequent
// addPieces call to the contract. This allows for a two-phase flow where:
//   - Phase 1 (this endpoint): Authorize and initiate piece fetching
//   - Phase 2 (contract call): Add pieces to the dataset with potentially different extraData
//
// # Workflow
//
//  1. Client calls POST /pdp/piece/fetch with piece CIDs and source URLs
//  2. Server validates extraData via eth_call (ensures authorization)
//  3. Server creates fetch tracking record and queues pieces for download
//  4. Background task (StorePiece) downloads pieces from source URLs
//  5. Client polls the same endpoint to check status (idempotent)
//  6. Once all pieces are "complete", client calls the contract to add pieces to dataset
//
// # Status Progression
//
// Each piece progresses through these statuses:
//
//   - pending: Piece is queued but download hasn't started
//   - inProgress: Download task is actively running (first attempt)
//   - retrying: Download task is running after one or more failures
//   - complete: Piece successfully downloaded and verified
//   - failed: Piece permanently failed after exhausting retries (currently 5 attempts)
//
// The overall response status reflects the worst-case across all pieces:
// failed > retrying > inProgress > pending > complete
//
// # Safety and Verification
//
// Several safety measures protect against malicious sources:
//
//   - Source URL validation: Must be HTTPS, path must match /piece/{pieceCid},
//     host must not be localhost/private IP/link-local
//
//   - Size limits: Piece size (encoded in PieceCIDv2) must not exceed PieceSizeLimit.
//     Downloads are capped at the declared size to prevent abuse.
//
//   - CommP verification: After download, the CommP (piece commitment) is computed
//     and verified against the expected value from PieceCIDv2. Mismatches are rejected.
//
//   - Size verification: Actual downloaded size must match the declared size.
//     Both truncation and oversized data are rejected.
//
// # Response Format
//
// Returns JSON with overall status and per-piece status:
//
//	{
//	  "status": "inProgress",
//	  "pieces": [
//	    {"pieceCid": "bafk...", "status": "complete"},
//	    {"pieceCid": "bafk...", "status": "inProgress"}
//	  ]
//	}
//
// # Idempotency
//
// Requests are idempotent based on (service, sha256(extraData), dataSetId, recordKeeper).
// Calling with the same parameters returns the current status without creating new work.
// This allows safe retries and status polling using the same request.
func (h *FetchHandler) HandleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Auth check
	service, err := h.auth.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		http.Error(w, "Validation error: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate recordKeeper against allowed list (for create-new case)
	var recordKeeperAddr common.Address
	if req.IsCreateNew() {
		recordKeeperAddr = common.HexToAddress(*req.RecordKeeper)
		if recordKeeperAddr == (common.Address{}) {
			http.Error(w, "Invalid recordKeeper address", http.StatusBadRequest)
			return
		}
		// Check recordKeeper is allowed (prevents bypass via malicious contract)
		if contract.IsPublicService(service) && !contract.IsRecordKeeperAllowed(recordKeeperAddr) {
			http.Error(w, "recordKeeper address not allowed for public service", http.StatusForbidden)
			return
		}
	}

	// Compute extraData hash and prepare idempotency key components
	extraDataBytes, err := decodeExtraData(&req.ExtraData)
	if err != nil {
		http.Error(w, "Invalid extraData: "+err.Error(), http.StatusBadRequest)
		return
	}
	if extraDataBytes == nil {
		http.Error(w, "extraData is required", http.StatusBadRequest)
		return
	}
	extraDataHash := sha256.Sum256(extraDataBytes)

	// Build idempotency key components
	var dataSetId uint64
	if req.DataSetId != nil {
		dataSetId = *req.DataSetId
	}
	recordKeeperStr := ""
	if req.RecordKeeper != nil {
		recordKeeperStr = *req.RecordKeeper
	}

	// Check idempotency - if we've seen this request before, return current status
	existingFetch, err := h.store.GetFetchByKey(ctx, service, extraDataHash[:], dataSetId, recordKeeperStr)
	if err != nil {
		log.Errorw("failed to check fetch idempotency", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if existingFetch != nil {
		// Return existing status
		h.respondWithStatus(ctx, w, existingFetch.ID)
		return
	}

	// Parse all piece CIDs (validates PieceCIDv2 format, extracts v1, size info)
	pieceInfos := make([]*PieceCidInfo, len(req.Pieces))
	pieceData := make([]contract.CidsCid, len(req.Pieces))
	for i, piece := range req.Pieces {
		info, err := ParsePieceCidV2(piece.PieceCid)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid pieceCid[%d]: %s", i, err.Error()), http.StatusBadRequest)
			return
		}
		if info.RawSize > uint64(PieceSizeLimit) {
			http.Error(w, fmt.Sprintf("pieceCid[%d]: size %d exceeds maximum %d", i, info.RawSize, PieceSizeLimit), http.StatusBadRequest)
			return
		}
		pieceInfos[i] = info
		pieceData[i] = contract.CidsCid{Data: info.CidV2.Bytes()}
	}

	// Validate extraData via eth_call
	validatorParams := &AddPiecesValidatorParams{
		DataSetId:    big.NewInt(int64(dataSetId)),
		RecordKeeper: recordKeeperAddr,
		PieceData:    pieceData,
		ExtraData:    extraDataBytes,
	}
	if err := h.validator.ValidateAddPieces(ctx, validatorParams); err != nil {
		http.Error(w, "extraData validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Build fetch pieces for database storage (v1 CID + raw size for v2 reconstruction)
	fetchPieces := make([]FetchPiece, len(pieceInfos))
	for i, info := range pieceInfos {
		fetchPieces[i] = FetchPiece{CidV1: info.CidV1, RawSize: info.RawSize}
	}

	// Create fetch record
	fetchRecord := &FetchRecord{
		Service:       service,
		ExtraDataHash: extraDataHash[:],
		DataSetId:     dataSetId,
		RecordKeeper:  recordKeeperStr,
	}

	fetchID, err := h.store.CreateFetchWithPieces(ctx, fetchRecord, fetchPieces)
	if err != nil {
		log.Errorw("failed to create fetch record", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// For each piece, check if it exists; if not, create parked_pieces entry
	for i, info := range pieceInfos {
		sourceURL := req.Pieces[i].SourceURL
		cidV1Str := info.CidV1.String()

		entry := &ParkedPieceEntry{
			PieceCid:        cidV1Str,
			PiecePaddedSize: padPieceSize(int64(info.RawSize)),
			PieceRawSize:    int64(info.RawSize),
			DataURL:         sourceURL,
		}

		created, err := h.store.CreateParkedPieceIfNotExists(ctx, entry)
		if err != nil {
			log.Errorw("failed to create parked piece", "error", err, "pieceCid", cidV1Str)
			// Continue with other pieces - partial success is acceptable
			continue
		}

		if created {
			log.Infow("created parked piece for fetch", "pieceCid", cidV1Str, "sourceUrl", sourceURL)
		} else {
			log.Debugw("parked piece already exists", "pieceCid", cidV1Str)
		}
	}

	// Return status
	h.respondWithStatus(ctx, w, fetchID)
}

// respondWithStatus queries piece statuses and returns a JSON response
func (h *FetchHandler) respondWithStatus(ctx context.Context, w http.ResponseWriter, fetchID int64) {
	// Get pieces for this fetch
	pieces, err := h.store.GetFetchPieces(ctx, fetchID)
	if err != nil {
		log.Errorw("failed to get fetch pieces", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Extract v1 CIDs for status lookup
	cidV1s := make([]cid.Cid, len(pieces))
	for i, p := range pieces {
		cidV1s[i] = p.CidV1
	}

	// Get statuses for all pieces (keyed by v1 CID string)
	statuses, err := h.store.GetPieceStatuses(ctx, cidV1s)
	if err != nil {
		log.Errorw("failed to get piece statuses", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Build response with v2 CIDs for API
	resp := &FetchResponse{
		Pieces: make([]FetchPieceStatus, len(pieces)),
	}

	for i, piece := range pieces {
		// Determine status based on fetch item and parked piece state
		status := h.determinePieceStatus(ctx, fetchID, piece, statuses[piece.CidV1.String()])

		// Reconstruct v2 CID for API response
		cidV2, err := commcid.PieceCidV2FromV1(piece.CidV1, piece.RawSize)
		if err != nil {
			log.Errorw("failed to reconstruct v2 CID", "error", err, "cidV1", piece.CidV1.String())
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		resp.Pieces[i] = FetchPieceStatus{
			PieceCid: cidV2.String(),
			Status:   status,
		}
	}

	resp.ComputeOverallStatus()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Errorw("failed to encode response", "error", err)
	}
}

// determinePieceStatus determines the status of a piece based on fetch item and parked piece state
func (h *FetchHandler) determinePieceStatus(ctx context.Context, fetchID int64, piece FetchPiece, ps *PieceStatus) FetchStatus {
	cidV1Str := piece.CidV1.String()

	// Already marked failed in fetch_items
	if piece.Failed {
		return FetchStatusFailed
	}

	// Piece not in parked_pieces - still pending initial queue
	if ps == nil {
		return FetchStatusPending
	}

	// Complete
	if ps.Complete {
		return FetchStatusComplete
	}

	// Has a task assigned
	if ps.TaskID != nil {
		if ps.TaskExists {
			// Task is alive
			if ps.Retries > 0 {
				return FetchStatusRetrying
			}
			return FetchStatusInProgress
		}

		// Task was deleted (orphaned) - check if it failed permanently
		exhausted, reason, err := h.store.CheckTaskExhaustedRetries(ctx, *ps.TaskID)
		if err != nil {
			log.Errorw("failed to check task history", "error", err, "taskId", *ps.TaskID)
			return FetchStatusPending
		}

		if exhausted {
			// Mark as failed in our tracking table
			if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, reason); err != nil {
				log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
			}
			return FetchStatusFailed
		}

		// Task deleted but not due to exhausted retries - history may have been purged
		// or task was manually deleted. Either way, piece is stuck (poller won't pick it
		// up because task_id is still set). Mark as failed.
		if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, "task orphaned without failure record"); err != nil {
			log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
		}
		return FetchStatusFailed
	}

	// No task assigned yet - waiting for task poller to pick it up
	return FetchStatusPending
}

// padPieceSize calculates the padded piece size from raw size using FR32 padding.
// FR32 encoding: 127 bytes of data become 128 bytes, then rounded up to next power of 2.
func padPieceSize(rawSize int64) int64 {
	if rawSize == 0 {
		return 0
	}
	// Apply FR32 ratio: ceil(rawSize * 128 / 127)
	fr32Padded := (uint64(rawSize)*128 + 126) / 127
	// Round up to next power of 2
	if fr32Padded == 0 {
		return 1
	}
	fr32Padded--
	return int64(1 << bits.Len64(fr32Padded))
}

// dbFetchStore implements FetchStore using harmonydb
type dbFetchStore struct {
	db *harmonydb.DB
}

// NewDBFetchStore creates a FetchStore backed by harmonydb
func NewDBFetchStore(db *harmonydb.DB) FetchStore {
	return &dbFetchStore{db: db}
}

func (s *dbFetchStore) GetFetchByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*FetchRecord, error) {
	var records []struct {
		ID            int64  `db:"id"`
		Service       string `db:"service"`
		ExtraDataHash []byte `db:"extra_data_hash"`
		DataSetId     uint64 `db:"data_set_id"`
		RecordKeeper  string `db:"record_keeper"`
	}

	err := s.db.Select(ctx, &records, `
		SELECT id, service, extra_data_hash, data_set_id, record_keeper
		FROM pdp_piece_fetches
		WHERE service = $1 AND extra_data_hash = $2 AND data_set_id = $3 AND record_keeper = $4
	`, service, hash, dataSetId, recordKeeper)
	if err != nil {
		return nil, fmt.Errorf("query fetch by key: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}

	return &FetchRecord{
		ID:            records[0].ID,
		Service:       records[0].Service,
		ExtraDataHash: records[0].ExtraDataHash,
		DataSetId:     records[0].DataSetId,
		RecordKeeper:  records[0].RecordKeeper,
	}, nil
}

func (s *dbFetchStore) CreateFetchWithPieces(ctx context.Context, fetch *FetchRecord, pieces []FetchPiece) (int64, error) {
	var fetchID int64

	_, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert fetch record and get the auto-generated ID
		err := tx.QueryRow(`
			INSERT INTO pdp_piece_fetches (service, extra_data_hash, data_set_id, record_keeper)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, fetch.Service, fetch.ExtraDataHash, fetch.DataSetId, fetch.RecordKeeper).Scan(&fetchID)
		if err != nil {
			return false, fmt.Errorf("insert fetch: %w", err)
		}

		// Insert piece items with raw size for v2 reconstruction
		for _, piece := range pieces {
			_, err := tx.Exec(`
				INSERT INTO pdp_piece_fetch_items (fetch_id, piece_cid, piece_raw_size)
				VALUES ($1, $2, $3)
			`, fetchID, piece.CidV1.String(), piece.RawSize)
			if err != nil {
				return false, fmt.Errorf("insert fetch item: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	return fetchID, err
}

func (s *dbFetchStore) GetPieceStatuses(ctx context.Context, pieceCids []cid.Cid) (map[string]*PieceStatus, error) {
	if len(pieceCids) == 0 {
		return make(map[string]*PieceStatus), nil
	}

	result := make(map[string]*PieceStatus)

	for _, pieceCid := range pieceCids {
		cidStr := pieceCid.String()
		var pieces []struct {
			PieceCid   string `db:"piece_cid"`
			Complete   bool   `db:"complete"`
			TaskID     *int64 `db:"task_id"`
			TaskExists bool   `db:"task_exists"`
			Retries    int    `db:"retries"`
		}

		// Join with harmony_task to get retry count and check if task exists
		err := s.db.Select(ctx, &pieces, `
			SELECT pp.piece_cid, pp.complete, pp.task_id,
			       (ht.id IS NOT NULL) as task_exists,
			       COALESCE(ht.retries, 0) as retries
			FROM parked_pieces pp
			LEFT JOIN harmony_task ht ON pp.task_id = ht.id
			WHERE pp.piece_cid = $1 AND pp.long_term = TRUE AND pp.cleanup_task_id IS NULL
		`, cidStr)
		if err != nil {
			return nil, fmt.Errorf("query piece status: %w", err)
		}

		if len(pieces) > 0 {
			result[cidStr] = &PieceStatus{
				PieceCid:   pieces[0].PieceCid,
				Complete:   pieces[0].Complete,
				TaskID:     pieces[0].TaskID,
				TaskExists: pieces[0].TaskExists,
				Retries:    pieces[0].Retries,
			}
		}
	}

	return result, nil
}

func (s *dbFetchStore) CreateParkedPieceIfNotExists(ctx context.Context, entry *ParkedPieceEntry) (bool, error) {
	var created bool

	_, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Check if piece already exists
		var existingID int64
		err := tx.QueryRow(`
			SELECT id FROM parked_pieces
			WHERE piece_cid = $1 AND long_term = TRUE AND cleanup_task_id IS NULL
		`, entry.PieceCid).Scan(&existingID)

		if err == nil {
			// Piece exists
			created = false
			return true, nil
		}

		// Create new parked piece
		var parkedPieceID int64
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, $2, $3, TRUE)
			RETURNING id
		`, entry.PieceCid, entry.PiecePaddedSize, entry.PieceRawSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("insert parked piece: %w", err)
		}

		// Create piece ref with data URL
		_, err = tx.Exec(`
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
			VALUES ($1, $2, TRUE)
		`, parkedPieceID, entry.DataURL)
		if err != nil {
			return false, fmt.Errorf("insert parked piece ref: %w", err)
		}

		created = true
		return true, nil
	}, harmonydb.OptionRetry())

	return created, err
}

func (s *dbFetchStore) GetFetchPieces(ctx context.Context, fetchID int64) ([]FetchPiece, error) {
	var items []struct {
		PieceCid     string  `db:"piece_cid"`
		PieceRawSize uint64  `db:"piece_raw_size"`
		Failed       bool    `db:"failed"`
		FailReason   *string `db:"fail_reason"`
	}

	err := s.db.Select(ctx, &items, `
		SELECT piece_cid, piece_raw_size, failed, fail_reason
		FROM pdp_piece_fetch_items WHERE fetch_id = $1
	`, fetchID)
	if err != nil {
		return nil, fmt.Errorf("query fetch items: %w", err)
	}

	result := make([]FetchPiece, len(items))
	for i, item := range items {
		c, err := cid.Parse(item.PieceCid)
		if err != nil {
			return nil, fmt.Errorf("parse CID %q: %w", item.PieceCid, err)
		}
		failReason := ""
		if item.FailReason != nil {
			failReason = *item.FailReason
		}
		result[i] = FetchPiece{
			CidV1:      c,
			RawSize:    item.PieceRawSize,
			Failed:     item.Failed,
			FailReason: failReason,
		}
	}

	return result, nil
}

func (s *dbFetchStore) MarkPieceFailed(ctx context.Context, fetchID int64, pieceCid string, reason string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE pdp_piece_fetch_items
		SET failed = TRUE, fail_reason = $3
		WHERE fetch_id = $1 AND piece_cid = $2
	`, fetchID, pieceCid, reason)
	if err != nil {
		return fmt.Errorf("mark piece failed: %w", err)
	}
	return nil
}

func (s *dbFetchStore) CheckTaskExhaustedRetries(ctx context.Context, taskID int64) (bool, string, error) {
	// Look up the last history entry for this task to check if it failed
	var history []struct {
		Result bool    `db:"result"` // true = success, false = failure
		Err    *string `db:"err"`    // error message when result is false
	}

	err := s.db.Select(ctx, &history, `
		SELECT result, err FROM harmony_task_history
		WHERE task_id = $1
		ORDER BY work_end DESC
		LIMIT 1
	`, taskID)
	if err != nil {
		return false, "", fmt.Errorf("query task history: %w", err)
	}

	if len(history) == 0 {
		// No history found - task may have been cleaned up or never ran
		return false, "", nil
	}

	// result=false means the task failed
	if !history[0].Result {
		reason := "unknown error"
		if history[0].Err != nil {
			reason = *history[0].Err
		}
		return true, reason, nil
	}

	return false, "", nil
}
