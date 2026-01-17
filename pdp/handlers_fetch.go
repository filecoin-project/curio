package pdp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// getPDPSenderAddress retrieves the PDP key address from the database
func getPDPSenderAddress(ctx context.Context, db *harmonydb.DB) (common.Address, error) {
	var privateKeyData []byte
	err := db.QueryRow(ctx,
		`SELECT private_key FROM eth_keys WHERE role = 'pdp'`).Scan(&privateKeyData)
	if err != nil {
		return common.Address{}, fmt.Errorf("fetching pdp private key from db: %w", err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyData)
	if err != nil {
		return common.Address{}, fmt.Errorf("converting private key: %w", err)
	}

	return crypto.PubkeyToAddress(privateKey.PublicKey), nil
}

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
	SourceURL  string // external SP URL to fetch from
	Failed     bool   // true if piece permanently failed
	FailReason string // error message when failed
}

// FetchItemStatus represents the status of a fetch item
type FetchItemStatus struct {
	TaskID     *int64 // task_id from pdp_piece_fetch_items
	TaskExists bool   // true if task_id exists in harmony_task
	Retries    int    // retry count from harmony_task (0 if task doesn't exist)
	Failed     bool   // true if piece permanently failed
}

// FetchStore abstracts database operations for the fetch handler
type FetchStore interface {
	// GetFetchByKey retrieves a fetch record by its idempotency key
	GetFetchByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*FetchRecord, error)

	// CreateFetchWithPieces creates a fetch record and its associated piece items in a transaction
	// Returns the created fetch ID
	CreateFetchWithPieces(ctx context.Context, fetch *FetchRecord, pieces []FetchPiece) (int64, error)

	// GetPieceStatuses retrieves the status of multiple pieces from parked_pieces (keyed by v1 CID string)
	// This checks if pieces have been successfully stored (complete=true)
	GetPieceStatuses(ctx context.Context, pieceCids []cid.Cid) (map[string]*PieceStatus, error)

	// GetFetchItemStatuses retrieves the status of fetch items (keyed by v1 CID string)
	// This checks the fetch task state (task_id, failed)
	GetFetchItemStatuses(ctx context.Context, fetchID int64, pieceCids []cid.Cid) (map[string]*FetchItemStatus, error)

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
	From         common.Address // sender address for eth_call simulation
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

	// eth_call to validate
	contractAddr := contract.ContractAddresses().PDPVerifier
	value := big.NewInt(0)
	if isCreateNew {
		// Sybil fee only required for create-new case
		value = contract.SybilFee()
	}
	msg := ethereum.CallMsg{
		From:  params.From,
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
	auth       Auth
	store      FetchStore
	validator  AddPiecesValidator
	senderAddr common.Address // cached PDP sender address for eth_call simulation
}

// NewFetchHandler creates a new FetchHandler
func NewFetchHandler(auth Auth, store FetchStore, validator AddPiecesValidator, senderAddr common.Address) *FetchHandler {
	return &FetchHandler{
		auth:       auth,
		store:      store,
		validator:  validator,
		senderAddr: senderAddr,
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

	// If we've seen this request before, return current status
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

	// Validate extraData via eth_call (use cached sender address for simulation)
	validatorParams := &AddPiecesValidatorParams{
		From:         h.senderAddr,
		DataSetId:    big.NewInt(int64(dataSetId)),
		RecordKeeper: recordKeeperAddr,
		PieceData:    pieceData,
		ExtraData:    extraDataBytes,
	}
	if err := h.validator.ValidateAddPieces(ctx, validatorParams); err != nil {
		http.Error(w, "extraData validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Build fetch pieces for database storage (v1 CID + raw size + source URL for task to pick up)
	fetchPieces := make([]FetchPiece, len(pieceInfos))
	for i, info := range pieceInfos {
		fetchPieces[i] = FetchPiece{
			CidV1:     info.CidV1,
			RawSize:   info.RawSize,
			SourceURL: req.Pieces[i].SourceURL,
		}
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

	// PDPFetchPieceTask will pick up items from pdp_piece_fetch_items,
	// download pieces, verify CommP, and create parked_pieces entries.

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

	// Get parked_pieces statuses (keyed by v1 CID string)
	pieceStatuses, err := h.store.GetPieceStatuses(ctx, cidV1s)
	if err != nil {
		log.Errorw("failed to get piece statuses", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Get fetch item statuses (keyed by v1 CID string)
	fetchItemStatuses, err := h.store.GetFetchItemStatuses(ctx, fetchID, cidV1s)
	if err != nil {
		log.Errorw("failed to get fetch item statuses", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Build response with v2 CIDs for API
	resp := &FetchResponse{
		Pieces: make([]FetchPieceStatus, len(pieces)),
	}

	for i, piece := range pieces {
		// Determine status based on fetch item and parked piece state
		cidV1Str := piece.CidV1.String()
		status := h.determinePieceStatus(ctx, fetchID, piece, fetchItemStatuses[cidV1Str], pieceStatuses[cidV1Str])

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

// determinePieceStatus determines the status of a piece based on fetch item and parked piece state.
//
// Status priority:
// 1. Check fetch item failed flag: if true, return failed
// 2. Check fetch item task_id: if not null, the fetch task is running
// 3. Check parked_pieces complete: if true, return complete
// 4. Check parked_pieces task_id: if not null, StorePiece is running
// 5. Otherwise return pending (waiting for fetch task to pick up)
func (h *FetchHandler) determinePieceStatus(ctx context.Context, fetchID int64, piece FetchPiece, fis *FetchItemStatus, ps *PieceStatus) FetchStatus {
	cidV1Str := piece.CidV1.String()

	// Already marked failed in fetch_items (from FetchPiece struct)
	if piece.Failed {
		return FetchStatusFailed
	}

	// Check fetch item status (PDPFetchPieceTask)
	if fis != nil {
		if fis.Failed {
			return FetchStatusFailed
		}

		// Fetch task is assigned
		if fis.TaskID != nil {
			if fis.TaskExists {
				// Task is alive
				if fis.Retries > 0 {
					return FetchStatusRetrying
				}
				return FetchStatusInProgress
			}

			// Task was deleted (orphaned), check if it failed permanently
			exhausted, reason, err := h.store.CheckTaskExhaustedRetries(ctx, *fis.TaskID)
			if err != nil {
				log.Errorw("failed to check task history", "error", err, "taskId", *fis.TaskID)
				return FetchStatusPending
			}

			if exhausted {
				// Mark as failed in our tracking table
				if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, reason); err != nil {
					log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
				}
				return FetchStatusFailed
			}

			// Task deleted but not due to exhausted retries, mark as failed
			if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, "fetch task orphaned without failure record"); err != nil {
				log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
			}
			return FetchStatusFailed
		}
	}

	// Fetch task completed successfully, check parked_pieces status
	if ps != nil {
		// Complete
		if ps.Complete {
			return FetchStatusComplete
		}

		// StorePiece task is assigned
		if ps.TaskID != nil {
			if ps.TaskExists {
				// Task is alive
				if ps.Retries > 0 {
					return FetchStatusRetrying
				}
				return FetchStatusInProgress
			}

			// StorePiece task was deleted, check if it failed permanently
			exhausted, reason, err := h.store.CheckTaskExhaustedRetries(ctx, *ps.TaskID)
			if err != nil {
				log.Errorw("failed to check task history", "error", err, "taskId", *ps.TaskID)
				return FetchStatusPending
			}

			if exhausted {
				// Mark as failed
				if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, "StorePiece: "+reason); err != nil {
					log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
				}
				return FetchStatusFailed
			}

			// StorePiece task orphaned, mark as failed
			if err := h.store.MarkPieceFailed(ctx, fetchID, cidV1Str, "StorePiece task orphaned without failure record"); err != nil {
				log.Errorw("failed to mark piece as failed", "error", err, "pieceCid", cidV1Str)
			}
			return FetchStatusFailed
		}

		// parked_pieces exists but task_id is NULL and not complete, waiting for StorePiece to pick up
		return FetchStatusInProgress
	}

	// No parked_pieces yet, waiting for fetch task to pick up
	return FetchStatusPending
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

		// Insert piece items with raw size and source_url for task to pick up
		for _, piece := range pieces {
			_, err := tx.Exec(`
				INSERT INTO pdp_piece_fetch_items (fetch_id, piece_cid, piece_raw_size, source_url)
				VALUES ($1, $2, $3, $4)
			`, fetchID, piece.CidV1.String(), piece.RawSize, piece.SourceURL)
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

	// Build array of CID strings for batch query
	cidStrs := make([]string, len(pieceCids))
	for i, c := range pieceCids {
		cidStrs[i] = c.String()
	}

	var pieces []struct {
		PieceCid   string `db:"piece_cid"`
		Complete   bool   `db:"complete"`
		TaskID     *int64 `db:"task_id"`
		TaskExists bool   `db:"task_exists"`
		Retries    int    `db:"retries"`
	}

	// Batch query with ANY() for all CIDs at once
	err := s.db.Select(ctx, &pieces, `
		SELECT pp.piece_cid, pp.complete, pp.task_id,
		       (ht.id IS NOT NULL) as task_exists,
		       COALESCE(ht.retries, 0) as retries
		FROM parked_pieces pp
		LEFT JOIN harmony_task ht ON pp.task_id = ht.id
		WHERE pp.piece_cid = ANY($1) AND pp.long_term = TRUE AND pp.cleanup_task_id IS NULL
	`, cidStrs)
	if err != nil {
		return nil, fmt.Errorf("query piece statuses: %w", err)
	}

	result := make(map[string]*PieceStatus)
	for _, p := range pieces {
		result[p.PieceCid] = &PieceStatus{
			PieceCid:   p.PieceCid,
			Complete:   p.Complete,
			TaskID:     p.TaskID,
			TaskExists: p.TaskExists,
			Retries:    p.Retries,
		}
	}

	return result, nil
}

func (s *dbFetchStore) GetFetchItemStatuses(ctx context.Context, fetchID int64, pieceCids []cid.Cid) (map[string]*FetchItemStatus, error) {
	if len(pieceCids) == 0 {
		return make(map[string]*FetchItemStatus), nil
	}

	// Build array of CID strings for batch query
	cidStrs := make([]string, len(pieceCids))
	for i, c := range pieceCids {
		cidStrs[i] = c.String()
	}

	var items []struct {
		PieceCid   string `db:"piece_cid"`
		TaskID     *int64 `db:"task_id"`
		TaskExists bool   `db:"task_exists"`
		Retries    int    `db:"retries"`
		Failed     bool   `db:"failed"`
	}

	// Batch query with ANY() for all CIDs at once
	err := s.db.Select(ctx, &items, `
		SELECT fi.piece_cid, fi.task_id, fi.failed,
		       (ht.id IS NOT NULL) as task_exists,
		       COALESCE(ht.retries, 0) as retries
		FROM pdp_piece_fetch_items fi
		LEFT JOIN harmony_task ht ON fi.task_id = ht.id
		WHERE fi.fetch_id = $1 AND fi.piece_cid = ANY($2)
	`, fetchID, cidStrs)
	if err != nil {
		return nil, fmt.Errorf("query fetch item statuses: %w", err)
	}

	result := make(map[string]*FetchItemStatus)
	for _, item := range items {
		result[item.PieceCid] = &FetchItemStatus{
			TaskID:     item.TaskID,
			TaskExists: item.TaskExists,
			Retries:    item.Retries,
			Failed:     item.Failed,
		}
	}

	return result, nil
}

func (s *dbFetchStore) GetFetchPieces(ctx context.Context, fetchID int64) ([]FetchPiece, error) {
	var items []struct {
		PieceCid     string  `db:"piece_cid"`
		PieceRawSize uint64  `db:"piece_raw_size"`
		SourceURL    string  `db:"source_url"`
		Failed       bool    `db:"failed"`
		FailReason   *string `db:"fail_reason"`
	}

	err := s.db.Select(ctx, &items, `
		SELECT piece_cid, piece_raw_size, source_url, failed, fail_reason
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
			SourceURL:  item.SourceURL,
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
		// Task may have been cleaned up or never ran
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
