package pdp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multicodec"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	types2 "github.com/filecoin-project/lotus/chain/types"
)

// PDPRoutePath is the base path for PDP routes
const PDPRoutePath = "/pdp"

// PDPService represents the service for managing data sets and pieces
type PDPService struct {
	Auth
	db      *harmonydb.DB
	storage paths.StashStore

	sender    *message.SenderETH
	ethClient *ethclient.Client
	filClient PDPServiceNodeApi
}

type PDPServiceNodeApi interface {
	ChainHead(ctx context.Context) (*types2.TipSet, error)
}

// NewPDPService creates a new instance of PDPService with the provided stores
func NewPDPService(db *harmonydb.DB, stor paths.StashStore, ec *ethclient.Client, fc PDPServiceNodeApi, sn *message.SenderETH) *PDPService {
	return &PDPService{
		Auth:    &NullAuth{},
		db:      db,
		storage: stor,

		sender:    sn,
		ethClient: ec,
		filClient: fc,
	}
}

// Routes registers the HTTP routes with the provided router
func Routes(r *chi.Mux, p *PDPService) {
	// Routes for data sets
	r.Route(path.Join(PDPRoutePath, "/data-sets"), func(r chi.Router) {
		// POST /pdp/data-sets - Create a new data set
		r.Post("/", p.handleCreateDataSet)

		// GET /pdp/data-sets/created/{txHash} - Get the status of a data set creation
		r.Get("/created/{txHash}", p.handleGetDataSetCreationStatus)

		// Individual data set routes
		r.Route("/{dataSetId}", func(r chi.Router) {
			// GET /pdp/data-sets/{set-id}
			r.Get("/", p.handleGetDataSet)

			// DEL /pdp/data-sets/{set-id}
			r.Delete("/", p.handleDeleteDataSet)

			// Routes for pieces within a data set
			r.Route("/pieces", func(r chi.Router) {
				// POST /pdp/data-sets/{set-id}/pieces
				r.Post("/", p.handleAddPieceToDataSet)

				// GET /pdp/data-sets/{set-id}/pieces/added/{txHash}
				r.Get("/added/{txHash}", p.handleGetPieceAdditionStatus)

				// Individual piece routes
				r.Route("/{pieceID}", func(r chi.Router) {
					// GET /pdp/data-sets/{set-id}/pieces/{piece-id}
					r.Get("/", p.handleGetDataSetPiece)

					// DEL /pdp/data-sets/{set-id}/pieces/{piece-id}
					r.Delete("/", p.handleDeleteDataSetPiece)
				})
			})
		})
	})

	r.Get(path.Join(PDPRoutePath, "/ping"), p.handlePing)

	// Routes for piece storage and retrieval
	// POST /pdp/piece
	r.Post(path.Join(PDPRoutePath, "/piece"), p.handlePiecePost)

	// GET /pdp/piece/
	r.Get(path.Join(PDPRoutePath, "/piece/"), p.handleFindPiece)

	// PUT /pdp/piece/upload/{uploadUUID}
	r.Put(path.Join(PDPRoutePath, "/piece/upload/{uploadUUID}"), p.handlePieceUpload)
}

// Handler functions

func (p *PDPService) handlePing(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	_, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Return 200 OK
	w.WriteHeader(http.StatusOK)
}

// handleCreateDataSet handles the creation of a new data set
func (p *PDPService) handleCreateDataSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Parse the request body to get the 'recordKeeper' address and extraData
	type RequestBody struct {
		RecordKeeper string  `json:"recordKeeper"`
		ExtraData    *string `json:"extraData,omitempty"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var reqBody RequestBody
	if err := json.Unmarshal(body, &reqBody); err != nil {
		http.Error(w, "Invalid JSON in request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.RecordKeeper == "" {
		http.Error(w, "recordKeeper address is required", http.StatusBadRequest)
		return
	}

	recordKeeperAddr := common.HexToAddress(reqBody.RecordKeeper)
	if recordKeeperAddr == (common.Address{}) {
		http.Error(w, "Invalid recordKeeper address", http.StatusBadRequest)
		return
	}

	// Check if the recordkeeper is in the whitelist for public services
	if contract.IsPublicService(serviceLabel) && !contract.IsRecordKeeperAllowed(recordKeeperAddr) {
		http.Error(w, "recordKeeper address not allowed for public service", http.StatusForbidden)
		return
	}

	// Decode extraData if provided
	extraDataBytes := []byte{}
	if reqBody.ExtraData != nil {
		extraDataHexStr := *reqBody.ExtraData
		decodedBytes, err := hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
		if err != nil {
			log.Errorf("Failed to decode hex extraData: %v", err)
			http.Error(w, "Invalid extraData format (must be hex encoded)", http.StatusBadRequest)
			return
		}
		extraDataBytes = decodedBytes
	}

	// Step 3: Get the sender address from 'eth_keys' table where role = 'pdp' limit 1
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		http.Error(w, "Failed to get sender address: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 4: Manually create the transaction without requiring a Signer
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Pack the method call data
	data, err := abiData.Pack("createDataSet", recordKeeperAddr, extraDataBytes)
	if err != nil {
		http.Error(w, "Failed to pack method call: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	tx := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		contract.SybilFee(),
		0,
		nil,
		data,
	)

	// Step 5: Send the transaction using SenderETH
	reason := "pdp-mkdataset"
	txHash, err := p.sender.Send(ctx, fromAddress, tx, reason)
	if err != nil {
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Step 6: Insert into message_waits_eth and pdp_data_set_creates
	txHashLower := strings.ToLower(txHash.Hex())
	log.Infow("PDP CreateDataSet: Inserting transaction tracking",
		"txHash", txHashLower,
		"service", serviceLabel,
		"recordKeeper", recordKeeperAddr.Hex())
	err = p.insertMessageWaitsAndDataSetCreate(ctx, txHashLower, serviceLabel)
	if err != nil {
		log.Errorf("Failed to insert into message_waits_eth and pdp_data_set_creates: %+v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Step 7: Respond with 201 Created and Location header
	w.Header().Set("Location", path.Join("/pdp/data-sets/created", txHashLower))
	w.WriteHeader(http.StatusCreated)
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

// insertMessageWaitsAndDataSetCreate inserts records into message_waits_eth and pdp_data_set_creates
func (p *PDPService) insertMessageWaitsAndDataSetCreate(ctx context.Context, txHashHex string, serviceLabel string) error {
	// Begin a database transaction
	_, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		log.Debugw("Inserting into message_waits_eth",
			"txHash", txHashHex,
			"status", "pending")
		_, err := tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashHex, "pending")
		if err != nil {
			log.Errorw("Failed to insert into message_waits_eth",
				"txHash", txHashHex,
				"error", err)
			return false, err // Return false to rollback the transaction
		}

		// Insert into pdp_data_set_creates
		log.Debugw("Inserting into pdp_data_set_creates",
			"txHash", txHashHex,
			"service", serviceLabel)
		_, err = tx.Exec(`
            INSERT INTO pdp_data_set_creates (create_message_hash, service)
            VALUES ($1, $2)
        `, txHashHex, serviceLabel)
		if err != nil {
			log.Errorw("Failed to insert into pdp_data_set_creates",
				"txHash", txHashHex,
				"error", err)
			return false, err // Return false to rollback the transaction
		}

		log.Infow("Successfully inserted orphaned transaction for watching",
			"txHash", txHashHex,
			"service", serviceLabel,
			"waiter_machine_id", "NULL")
		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	return nil
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
		http.Error(w, "Failed to query data set creation: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to query message status: "+err.Error(), http.StatusInternalServerError)
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
			http.Error(w, "Failed to query data set: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to retrieve data set: "+err.Error(), http.StatusInternalServerError)
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
		SubPieceCID     string `db:"sub_piece"`
		SubPieceOffset  int64  `db:"sub_piece_offset"`
		SubPieceSize    int64  `db:"sub_piece_size"`
		SubPieceRawSize uint64 `db:"sub_piece_raw_size"`
	}

	err = p.db.Select(ctx, &pieces, `
        SELECT
            dsp.piece_id,
            dsp.piece,
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
		http.Error(w, "Failed to retrieve data set pieces: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to retrieve next challenge epoch: "+err.Error(), http.StatusInternalServerError)
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

	// Calculate aggregate piece raw sizes by summing sub-piece raw sizes
	pieceRawSizes := make(map[string]uint64)
	for _, piece := range pieces {
		pieceRawSizes[piece.PieceCid] += piece.SubPieceRawSize
	}

	// Convert pieces to the desired JSON format
	for _, piece := range pieces {
		// Use the summed raw size for the aggregate piece
		aggregateRawSize := pieceRawSizes[piece.PieceCid]
		pcv2, _, err := asPieceCIDv2(piece.PieceCid, aggregateRawSize)
		if err != nil {
			http.Error(w, "Invalid PieceCID: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Use the raw size for the sub piece
		spcv2, _, err := asPieceCIDv2(piece.SubPieceCID, piece.SubPieceRawSize)
		if err != nil {
			http.Error(w, "Invalid SubPieceCID: "+err.Error(), http.StatusBadRequest)
			return
		}
		response.Pieces = append(response.Pieces, PieceEntry{
			PieceID:        piece.PieceID,
			PieceCID:       pcv2.String(),
			SubPieceCID:    spcv2.String(),
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

func (p *PDPService) handleDeleteDataSet(w http.ResponseWriter, r *http.Request) {
	// ### DEL /data-sets/{set id}
	// Remove the specified data set entirely

	http.Error(w, "todo", http.StatusBadRequest)
}

func (p *PDPService) handleAddPieceToDataSet(w http.ResponseWriter, r *http.Request) {
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
	dataSetIdUint64, err := strconv.ParseUint(dataSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	// check if the data set belongs to the service in pdp_data_sets

	var dataSetService string
	err = p.db.QueryRow(ctx, `
			SELECT service
			FROM pdp_data_sets
			WHERE id = $1
		`, dataSetIdUint64).Scan(&dataSetService)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to retrieve data set: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if dataSetService != serviceLabel {
		// same as when actually not found to avoid leaking information in obvious ways
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}

	// Convert dataSetId to *big.Int
	dataSetId := new(big.Int).SetUint64(dataSetIdUint64)

	// Step 3: Parse the request body
	type SubPieceEntry struct {
		SubPieceCID   string `json:"subPieceCid"`
		subPieceCIDv1 string
	}

	type AddPieceRequest struct {
		PieceCID   string `json:"pieceCid"`
		pieceCIDv1 string
		SubPieces  []SubPieceEntry `json:"subPieces"`
	}

	// AddPiecesPayload defines the structure for the entire add pieces request payload
	type AddPiecesPayload struct {
		Pieces    []AddPieceRequest `json:"pieces"`
		ExtraData *string           `json:"extraData,omitempty"`
	}

	var payload AddPiecesPayload
	err = json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	if len(payload.Pieces) == 0 {
		http.Error(w, "At least one piece must be provided", http.StatusBadRequest)
		return
	}

	extraDataBytes := []byte{}
	if payload.ExtraData != nil {
		extraDataHexStr := *payload.ExtraData
		decodedBytes, err := hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
		if err != nil {
			log.Errorf("Failed to decode hex extraData: %v", err)
			http.Error(w, "Invalid extraData format (must be hex encoded)", http.StatusBadRequest)
			return
		}
		extraDataBytes = decodedBytes
	}

	// Collect all subPieceCids to fetch their info in a batch
	subPieceCidSet := make(map[string]struct{})
	for _, addPieceReq := range payload.Pieces {
		if addPieceReq.PieceCID == "" {
			http.Error(w, "PieceCID is required for each piece", http.StatusBadRequest)
			return
		}

		if len(addPieceReq.SubPieces) == 0 {
			http.Error(w, "At least one subPiece is required per piece", http.StatusBadRequest)
			return
		}

		for i, subPieceEntry := range addPieceReq.SubPieces {
			if subPieceEntry.SubPieceCID == "" {
				http.Error(w, "subPieceCid is required for each subPiece", http.StatusBadRequest)
				return
			}
			pieceCid, err := asPieceCIDv1(subPieceEntry.SubPieceCID)
			if err != nil {
				http.Error(w, "Invalid SubPiece:"+err.Error(), http.StatusBadRequest)
				return
			}
			pieceCidString := pieceCid.String()

			addPieceReq.SubPieces[i].subPieceCIDv1 = pieceCidString // save it for to query subPieceInfoMap later

			if _, exists := subPieceCidSet[pieceCidString]; exists {
				http.Error(w, "duplicate subPieceCid in request", http.StatusBadRequest)
				return
			}

			subPieceCidSet[pieceCidString] = struct{}{}
		}
	}

	// Convert set to slice
	subPieceCidList := make([]string, 0, len(subPieceCidSet))
	for cidStr := range subPieceCidSet {
		subPieceCidList = append(subPieceCidList, cidStr)
	}

	// Map to store subPieceCID -> [pieceInfo, pdp_pieceref.id, subPieceOffset]
	type SubPieceInfo struct {
		PieceCIDv1     cid.Cid
		PaddedSize     abi.PaddedPieceSize
		RawSize        uint64 // RawSize is the size of the piece with no padding applied
		PDPPieceRefID  int64
		SubPieceOffset uint64
	}

	subPieceInfoMap := make(map[string]*SubPieceInfo)

	// Start a DB transaction
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Step 4: Get pdp_piecerefs matching all subPiece cids + make sure those refs belong to serviceLabel
		rows, err := tx.Query(`
            SELECT ppr.piece_cid, ppr.id AS pdp_pieceref_id, ppr.piece_ref,
                   pp.piece_padded_size, pp.piece_raw_size
            FROM pdp_piecerefs ppr
            JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
            JOIN parked_pieces pp ON pp.id = pprf.piece_id
            WHERE ppr.service = $1 AND ppr.piece_cid = ANY($2)
        `, serviceLabel, subPieceCidList)
		if err != nil {
			return false, err
		}
		defer rows.Close()

		foundSubPieces := make(map[string]struct{})
		for rows.Next() {
			var pieceCIDStr string
			var pdpPieceRefID, pieceRefID int64
			var piecePaddedSize uint64
			var pieceRawSize uint64

			err := rows.Scan(&pieceCIDStr, &pdpPieceRefID, &pieceRefID, &piecePaddedSize, &pieceRawSize)
			if err != nil {
				return false, err
			}

			// Parse the piece CID
			pieceCID, err := cid.Decode(pieceCIDStr)
			if err != nil {
				return false, fmt.Errorf("invalid piece CID in database: %s", pieceCIDStr)
			}

			subPieceInfoMap[pieceCIDStr] = &SubPieceInfo{
				PieceCIDv1:     pieceCID,
				PaddedSize:     abi.PaddedPieceSize(piecePaddedSize),
				RawSize:        pieceRawSize,
				PDPPieceRefID:  pdpPieceRefID,
				SubPieceOffset: 0, // Will compute offset later
			}

			foundSubPieces[pieceCIDStr] = struct{}{}
		}

		// Check if all subPiece CIDs were found
		for _, cidStr := range subPieceCidList {
			if _, found := foundSubPieces[cidStr]; !found {
				return false, fmt.Errorf("subPiece CID %s not found or does not belong to service %s", cidStr, serviceLabel)
			}
		}

		// Now, for each AddPieceRequest, validate PieceCid and prepare data for ETH transaction
		for i, addPieceReq := range payload.Pieces {
			// Collect pieceInfos for subPieces
			pieceInfos := make([]abi.PieceInfo, len(addPieceReq.SubPieces))

			var totalOffset uint64 = 0
			for j, subPieceEntry := range addPieceReq.SubPieces {
				subPieceInfo, exists := subPieceInfoMap[subPieceEntry.subPieceCIDv1]
				if !exists {
					return false, fmt.Errorf("subPiece CID %s not found in subPiece info map", subPieceEntry.subPieceCIDv1)
				}

				// Update SubPieceOffset
				subPieceInfo.SubPieceOffset = totalOffset
				subPieceInfoMap[subPieceEntry.subPieceCIDv1] = subPieceInfo // Update the map

				pieceInfos[j] = abi.PieceInfo{
					Size:     subPieceInfo.PaddedSize,
					PieceCID: subPieceInfo.PieceCIDv1,
				}

				totalOffset += uint64(subPieceInfo.PaddedSize)
			}

			// Use GenerateUnsealedCID to generate PieceCid from subPieces
			proofType := abi.RegisteredSealProof_StackedDrg64GiBV1_1 // Proof type sets max piece size, nothing else
			generatedPieceCid, err := nonffi.GenerateUnsealedCID(proofType, pieceInfos)
			if err != nil {
				return false, fmt.Errorf("failed to generate PieceCid: %v", err)
			}

			// Compare generated PieceCid with provided PieceCid
			providedPieceCidv1, err := asPieceCIDv1(addPieceReq.PieceCID)
			if err != nil {
				return false, fmt.Errorf("invalid provided PieceCid: %v", err)
			}
			payload.Pieces[i].pieceCIDv1 = providedPieceCidv1.String()

			if !providedPieceCidv1.Equals(generatedPieceCid) {
				return false, fmt.Errorf("provided PieceCid does not match generated PieceCid: %s != %s", providedPieceCidv1, generatedPieceCid)
			}
		}

		// All validations passed, commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		http.Error(w, "Failed to validate subPieces: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Step 5: Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepare PieceData array for Ethereum transaction
	// Define a Struct that matches the Solidity PieceData struct
	type PieceData struct {
		Data []byte // CID
	}

	var pieceDataArray []PieceData

	for _, addPieceReq := range payload.Pieces {
		// Convert PieceCid to bytes
		pieceCidV2, err := cid.Decode(addPieceReq.PieceCID)
		if err != nil {
			http.Error(w, "Invalid PieceCid: "+err.Error(), http.StatusBadRequest)
			return
		}
		_, rawSize, err := commcid.PieceCidV1FromV2(pieceCidV2)
		if err != nil {
			http.Error(w, "Invalid CommPv2:"+err.Error(), http.StatusBadRequest)
			return
		}
		height, _, err := commcid.PayloadSizeToV1TreeHeightAndPadding(rawSize)
		if err != nil {
			http.Error(w, "Computing height and padding:"+err.Error(), http.StatusBadRequest)
			return
		}
		if height > 50 {
			http.Error(w, "Invalid height", http.StatusBadRequest)
		}

		// Get raw size by summing up the sizes of subPieces
		var totalSize uint64 = 0
		prevSubPieceSize := subPieceInfoMap[addPieceReq.SubPieces[0].subPieceCIDv1].PaddedSize
		for i, subPieceEntry := range addPieceReq.SubPieces {
			subPieceInfo := subPieceInfoMap[subPieceEntry.subPieceCIDv1]
			if subPieceInfo.PaddedSize > prevSubPieceSize {
				msg := fmt.Sprintf("SubPieces must be in descending order of size, piece %d %s is larger than prev subPiece %s",
					i, subPieceEntry.SubPieceCID, addPieceReq.SubPieces[i-1].SubPieceCID)
				http.Error(w, msg, http.StatusBadRequest)
				return
			}

			prevSubPieceSize = subPieceInfo.PaddedSize
			totalSize += uint64(subPieceInfo.RawSize)
		}
		// sanity check that the rawSize in the CommPv2 matches the totalSize of the subPieces
		if rawSize != totalSize {
			http.Error(w, fmt.Sprintf("Raw size miss-match: expected %d, got %d", totalSize, rawSize), http.StatusBadRequest)
			return
		}

		/* TODO: this doesn't work, do we need it?
		// sanity check that height and totalSize match
		computedHeight := bits.LeadingZeros64(totalSize-1) - 5
		if computedHeight != int(height) {
			http.Error(w, fmt.Sprintf("Height miss-match: expected %d, got %d for total size %d", computedHeight, height, totalSize), http.StatusBadRequest)
		}
		*/

		// Prepare PieceData for Ethereum transaction
		pieceData := PieceData{
			Data: pieceCidV2.Bytes(),
		}

		pieceDataArray = append(pieceDataArray, pieceData)
	}

	// Step 6: Prepare the Ethereum transaction
	// Pack the method call data
	// The extraDataBytes variable is now correctly populated above
	data, err := abiData.Pack("addPieces", dataSetId, pieceDataArray, extraDataBytes)
	if err != nil {
		http.Error(w, "Failed to pack method call: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 7: Get the sender address from 'eth_keys' table where role = 'pdp' limit 1
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		http.Error(w, "Failed to get sender address: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	txEth := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	// Step 8: Send the transaction using SenderETH
	reason := "pdp-addpieces"
	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Step 9: Insert into message_waits_eth and pdp_data_set_pieces
	// Ensure consistent lowercase transaction hash
	txHashLower := strings.ToLower(txHash.Hex())
	log.Infow("PDP AddPieces: Inserting transaction tracking",
		"txHash", txHashLower,
		"dataSetId", dataSetIdUint64,
		"pieceCount", len(payload.Pieces))
	_, err = p.db.BeginTransaction(ctx, func(txdb *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		log.Debugw("Inserting AddPieces into message_waits_eth",
			"txHash", txHashLower,
			"status", "pending")
		_, err := txdb.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashLower, "pending")
		if err != nil {
			log.Errorw("Failed to insert AddPieces into message_waits_eth",
				"txHash", txHashLower,
				"error", err)
			return false, err // Return false to rollback the transaction
		}

		// Update data set for initialization upon first add
		_, err = txdb.Exec(`
			UPDATE pdp_data_sets SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
			`, dataSetIdUint64)
		if err != nil {
			return false, err
		}

		// Insert into pdp_data_set_pieces

		for addMessageIndex, addPieceReq := range payload.Pieces {
			for _, subPieceEntry := range addPieceReq.SubPieces {
				subPieceInfo := subPieceInfoMap[subPieceEntry.subPieceCIDv1]

				// Insert into pdp_data_set_pieces
				_, err = txdb.Exec(`
                    INSERT INTO pdp_data_set_piece_adds (
                        data_set,
                        piece,
                        add_message_hash,
                        add_message_index,
                        sub_piece,
                        sub_piece_offset,
                        sub_piece_size,
                        pdp_pieceref
                    )
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                `,
					dataSetIdUint64,
					addPieceReq.pieceCIDv1,
					txHashLower,
					addMessageIndex,
					subPieceEntry.subPieceCIDv1,
					subPieceInfo.SubPieceOffset,
					subPieceInfo.PaddedSize,
					subPieceInfo.PDPPieceRefID,
				)
				if err != nil {
					return false, err
				}
			}
		}

		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("Failed to insert into database", "error", err, "txHash", txHashLower, "subPieces", subPieceInfoMap)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Step 10: Respond with 201 Created
	w.Header().Set("Location", path.Join("/pdp/data-sets", dataSetIdStr, "pieces/added", txHashLower))
	w.WriteHeader(http.StatusCreated)
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
		http.Error(w, "Failed to retrieve data set: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to query piece additions: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to query transaction status: "+err.Error(), http.StatusInternalServerError)
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
		// Query pdp_data_set_pieces for confirmed pieces with their IDs
		pieceCids := make([]string, 0, len(uniquePieceMap))
		for piece := range uniquePieceMap {
			pieceCids = append(pieceCids, piece)
		}

		type ConfirmedPiece struct {
			PieceID uint64 `db:"piece_id"`
			Piece   string `db:"piece"`
		}

		var confirmedPieces []ConfirmedPiece
		err = p.db.Select(ctx, &confirmedPieces, `
			SELECT DISTINCT piece_id, piece
			FROM pdp_data_set_pieces
			WHERE data_set = $1 AND piece = ANY($2)
			ORDER BY piece_id
		`, dataSetId, pieceCids)
		if err != nil {
			log.Warnf("Failed to query confirmed pieces: %v", err)
			// Don't fail the request, just log the warning
		} else {
			// Extract just the piece IDs
			for _, cr := range confirmedPieces {
				confirmedPieceIds = append(confirmedPieceIds, cr.PieceID)
			}
		}
	}

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
		http.Error(w, "Failed to retrieve data set: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if dataSetService != serviceLabel {
		// same as when actually not found to avoid leaking information in obvious ways
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}

	// Get the ABI and pack the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Pack the method call data
	data, err := abiData.Pack("schedulePieceDeletions",
		big.NewInt(int64(dataSetId)),
		[]*big.Int{big.NewInt(int64(pieceID))},
		[]byte{},
	)
	if err != nil {
		http.Error(w, "Failed to pack method call: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the sender address
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		http.Error(w, "Failed to get sender address: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Schedule deletion of the piece from the data set using a transaction
	txHashLower := strings.ToLower(txHash.Hex())
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err := tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, $2)
		`, txHashLower, "pending")
		if err != nil {
			return false, err
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		http.Error(w, "Failed to schedule delete piece: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return 204 No Content on successful deletion
	w.WriteHeader(http.StatusNoContent)
}

func (p *PDPService) handleGetDataSetPiece(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
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
		http.Error(w, "Failed to retrieve piece: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to retrieve subPieces: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to encode response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func asPieceCIDv1(cidStr string) (cid.Cid, error) {
	pieceCid, err := cid.Decode(cidStr)
	if err != nil {
		return cid.Undef, fmt.Errorf("failed to decode PieceCID: %w", err)
	}
	if pieceCid.Prefix().MhType == uint64(multicodec.Fr32Sha256Trunc254Padbintree) {
		c1, _, err := commcid.PieceCidV1FromV2(pieceCid)
		return c1, err
	}
	return pieceCid, nil
}

// asPieceCIDv2 converts a string to a PieceCIDv2. Where the input is expected to be a PieceCIDv1,
// a size argument is required. Where it's expected to be a v2, the size argument is ignored. The
// size either derived from the v2 or from the size argument in the case of a v1 is returned.
func asPieceCIDv2(cidStr string, size uint64) (cid.Cid, uint64, error) {
	pieceCid, err := cid.Decode(cidStr)
	if err != nil {
		return cid.Undef, 0, fmt.Errorf("failed to decode subPieceCid: %w", err)
	}
	switch pieceCid.Prefix().MhType {
	case uint64(multicodec.Sha2_256Trunc254Padded):
		if size == 0 {
			return cid.Undef, 0, fmt.Errorf("size must be provided for PieceCIDv1")
		}
		c, err := commcid.PieceCidV2FromV1(pieceCid, size)
		if err != nil {
			return cid.Undef, 0, err
		}
		return c, size, nil
	case uint64(multicodec.Fr32Sha256Trunc254Padbintree):
		// get the size from the CID, not the argument
		_, size, err := commcid.PieceCidV2ToDataCommitment(pieceCid)
		if err != nil {
			return cid.Undef, 0, err
		}
		return pieceCid, size, nil
	default:
		return cid.Undef, 0, fmt.Errorf("unsupported piece CID type: %d", pieceCid.Prefix().MhType)
	}
}
