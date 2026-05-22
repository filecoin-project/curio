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

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
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

// AddPiecesValidatorParams contains parameters for eth_call validation
type AddPiecesValidatorParams struct {
	DataSetId    *big.Int // 0 for create-new
	RecordKeeper common.Address
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
	ethClient  ethchain.EthClient
	db         *harmonydb.DB
	senderAddr common.Address // cached, lazily loaded
}

// NewEthCallValidator creates a validator that uses eth_call
func NewEthCallValidator(ethClient ethchain.EthClient, db *harmonydb.DB) *EthCallValidator {
	return &EthCallValidator{ethClient: ethClient, db: db}
}

func (v *EthCallValidator) ValidateAddPieces(ctx context.Context, params *AddPiecesValidatorParams) error {
	// Lazily load sender address if not cached
	if v.senderAddr == (common.Address{}) && v.db != nil {
		addr, err := getPDPSenderAddress(ctx, v.db)
		if err != nil {
			return fmt.Errorf("failed to get PDP sender address: %w", err)
		}
		v.senderAddr = addr
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("failed to get contract ABI: %w", err)
	}

	// Build addPieces calldata
	// When dataSetId is 0 (create new), use recordKeeper address
	// When dataSetId > 0 (add to existing), use zero address for listener
	isCreateNew := params.DataSetId.Cmp(big.NewInt(0)) == 0
	listenerAddr := common.Address{}
	if isCreateNew {
		listenerAddr = params.RecordKeeper
	}

	data, err := abiData.Pack("addPieces", params.DataSetId, listenerAddr, params.PieceData, params.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to pack addPieces call: %w", err)
	}

	// eth_call to validate — match tx value used for dataset creation (no sybil fee)
	value := big.NewInt(0)
	msg := ethereum.CallMsg{
		From:  v.senderAddr,
		To:    new(contract.ContractAddresses().PDPVerifier),
		Data:  data,
		Value: value,
	}

	_, err = v.ethClient.CallContract(ctx, msg, nil)
	if err != nil {
		return fmt.Errorf("addPieces validation failed: %w", err)
	}

	return nil
}

// PullHandler handles piece pull requests
type PullHandler struct {
	auth      Auth
	store     PullStore
	validator AddPiecesValidator
}

// NewPullHandler creates a new PullHandler
func NewPullHandler(auth Auth, store PullStore, validator AddPiecesValidator) *PullHandler {
	return &PullHandler{
		auth:      auth,
		store:     store,
		validator: validator,
	}
}

// HandlePull handles POST /pdp/piece/pull requests for SP-to-SP piece pull.
//
// # Overview
//
// This endpoint allows a client to request that pieces be pulled from other storage
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
//   - pieces (required): Array of pieces to pull, each containing:
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
//     the status of the existing pull rather than creating duplicates.
//
// The extraData used here does NOT need to match the extraData used in the subsequent
// addPieces call to the contract. This allows for a two-phase flow where:
//   - Phase 1 (this endpoint): Authorize and initiate piece pulling
//   - Phase 2 (contract call): Add pieces to the dataset with potentially different extraData
//
// # Workflow
//
//  1. Client calls POST /pdp/piece/pull with piece CIDs and source URLs
//  2. Server validates extraData via eth_call (ensures authorization)
//  3. Server creates pull tracking records; duplicate pieces with different
//     source URLs are kept so PullPiece can try each supplied source.
//  4. PDPPullPieceTask groups work by piece, attaches pull refs to the active
//     long-term parked_pieces row, and downloads directly when it created that row.
//  5. Client polls the same endpoint to check status (idempotent)
//  6. Once all pieces are "complete", client calls the contract to add pieces to dataset
//
// # Status Progression
//
// Each piece progresses through these statuses:
//
//   - pending: Piece is queued or waiting on an existing parked_pieces row
//   - inProgress: PullPiece task is actively running
//   - retrying: The current task row has a retry count greater than zero
//   - complete: Piece successfully downloaded and verified
//   - failed: Piece permanently failed; the task enforces this through explicit
//     item state rather than inferring it from parked_pieces.
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
func (h *PullHandler) HandlePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpServerError(w, http.StatusMethodNotAllowed, "Method Not Allowed", nil)
		return
	}

	ctx := r.Context()

	// Auth check
	service, err := h.auth.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	// Parse request body
	var req PullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: "+err.Error(), err)
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		httpServerError(w, http.StatusBadRequest, "Validation error: "+err.Error(), err)
		return
	}

	// Validate recordKeeper against allowed list (for create-new case)
	var recordKeeperAddr common.Address
	if req.IsCreateNew() {
		recordKeeperAddr = common.HexToAddress(*req.RecordKeeper)
		if recordKeeperAddr == (common.Address{}) {
			httpServerError(w, http.StatusBadRequest, "Invalid recordKeeper address", err)
			return
		}
		// Check recordKeeper is allowed (prevents bypass via malicious contract)
		if contract.IsPublicService(service) && !contract.IsRecordKeeperAllowed(recordKeeperAddr) {
			httpServerError(w, http.StatusForbidden, "recordKeeper address not allowed for public service", err)
			return
		}
	}

	// Compute extraData hash and prepare idempotency key components
	extraDataBytes, err := decodeExtraData(&req.ExtraData)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid extraData: "+err.Error(), err)
		return
	}
	if extraDataBytes == nil {
		httpServerError(w, http.StatusBadRequest, "extraData is required", err)
		return
	}
	extraDataHash := sha256.Sum256(extraDataBytes)
	payer, err := FWSSPayerFromExtraData(extraDataBytes)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid extraData payer: "+err.Error(), err)
		return
	}

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
	existingPull, err := h.store.GetPullByKey(ctx, service, extraDataHash[:], dataSetId, recordKeeperStr)
	if err != nil {
		log.Errorw("failed to check pull idempotency", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Internal error", err)
		return
	}

	if existingPull != nil {
		// Return existing status
		h.respondWithStatus(ctx, w, existingPull.ID)
		return
	}

	// Parse all piece CIDs (validates PieceCIDv2 format, extracts v1, size info)
	pieceInfos := make([]*PieceCidInfo, len(req.Pieces))
	pieceData := make([]contract.CidsCid, len(req.Pieces))
	for i, piece := range req.Pieces {
		info, err := ParsePieceCidV2(piece.PieceCid)
		if err != nil {
			msg := fmt.Sprintf("Invalid pieceCid[%d]: %s", i, err.Error())
			httpServerError(w, http.StatusBadRequest, msg, err)

			return
		}
		if info.RawSize > uint64(PieceSizeLimit) {
			msg := fmt.Sprintf("pieceCid[%d]: size %d exceeds maximum %d", i, info.RawSize, PieceSizeLimit)
			httpServerError(w, http.StatusBadRequest, msg, err)
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
		httpServerError(w, http.StatusBadRequest, "extraData validation failed: "+err.Error(), err)
		return
	}

	// Build pull pieces for database storage (v1 CID + raw size + source URL for task to pick up)
	pullPieces := make([]PullPiece, len(pieceInfos))
	for i, info := range pieceInfos {
		pullPieces[i] = PullPiece{
			CidV1:     info.CidV1,
			RawSize:   info.RawSize,
			SourceURL: req.Pieces[i].SourceURL,
		}
	}

	// Create pull record
	pullRecord := &PullRecord{
		Service:       service,
		ExtraDataHash: extraDataHash[:],
		DataSetId:     dataSetId,
		RecordKeeper:  recordKeeperStr,
		ClientAddress: payer.Hex(),
	}

	pullID, backpressure, err := h.store.CreatePullWithPieces(ctx, pullRecord, pullPieces)
	if err != nil {
		log.Errorw("failed to create pull record", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Internal error", err)
		return
	}

	if backpressure {
		log.Warnw("pull queue backpressure", "client", pullRecord.ClientAddress)
		w.Header().Set("Retry-After", "60")
		http.Error(w, "pull queue backpressure", http.StatusTooManyRequests)
		return
	}

	// PDPPullPieceTask will pick up items from pdp_piece_pull_items, group them
	// by piece, attach refs to parked_pieces, and download when PullPiece owns
	// the parked row.

	// Return status
	h.respondWithStatus(ctx, w, pullID)
}

// respondWithStatus queries piece statuses and returns a JSON response
func (h *PullHandler) respondWithStatus(ctx context.Context, w http.ResponseWriter, pullID int64) {
	// Get pieces for this pull
	pieces, err := h.store.GetPullStatus(ctx, pullID)
	if err != nil {
		log.Errorw("failed to get pull pieces", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Internal error", err)
		return
	}

	resp := &PullResponse{
		Pieces: pieces,
	}

	resp.ComputeOverallStatus()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Errorw("failed to encode response", "error", err)
	}
}
