package pdp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

// handleCreateDataSetAndAddPieces handles the creation of a new data set and adding pieces at the same time
func (p *PDPService) handleCreateDataSetAndAddPieces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// We should use background context here for consistency.
	//	If some task is taking longer than expected, a client might time out and context will be canceled.
	//	This can result in an inconsistent state in the database where we created the dataSet but did not add to DB.

	workCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	type RequestBody struct {
		RecordKeeper string            `json:"recordKeeper"`
		Pieces       []AddPieceRequest `json:"pieces"`
		ExtraData    *string           `json:"extraData,omitempty"`
	}

	var reqBody RequestBody
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid JSON in request body: "+err.Error(), err)
		return
	}

	if reqBody.RecordKeeper == "" {
		httpServerError(w, http.StatusBadRequest, "recordKeeper address is required", err)
		return
	}

	recordKeeperAddr := common.HexToAddress(reqBody.RecordKeeper)
	if recordKeeperAddr == (common.Address{}) {
		httpServerError(w, http.StatusBadRequest, "Invalid recordKeeper address", err)
		return
	}

	// Check if the recordkeeper is in the whitelist for public services
	if contract.IsPublicService(serviceLabel) && !contract.IsRecordKeeperAllowed(recordKeeperAddr) {
		httpServerError(w, http.StatusForbidden, "recordKeeper address not allowed for public service", err)
		return
	}

	extraDataBytes, err := decodeExtraData(reqBody.ExtraData)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid extraData format (must be hex encoded)", err)
		return
	}
	if len(extraDataBytes) > MaxAddPiecesExtraDataSize {
		errMsg := fmt.Sprintf("extraData size (%d bytes) exceeds the maximum allowed limit for CreateDataSetAndAddPieces (%d bytes)", len(extraDataBytes), MaxAddPiecesExtraDataSize)
		httpServerError(w, http.StatusBadRequest, errMsg, err)
		return
	}

	// Check if indexing is needed by decoding the extraData
	mustIndex, err := CheckIfIndexingNeededFromExtraData(extraDataBytes)
	if err != nil {
		log.Warnw("Failed to check if indexing is needed from extraData, skipping indexing", "error", err)
		mustIndex = false
	}
	if mustIndex {
		log.Infow("ExtraData contains withIPFSIndexing metadata, pieces will be marked for indexing")
	}

	pieceDataArray, subPieceInfoMap, err := p.transformAddPiecesRequest(ctx, serviceLabel, reqBody.Pieces)
	if err != nil {
		log.Warnf("Failed to process AddPieces request data: %+v", err)
		httpServerError(w, http.StatusBadRequest, "Failed to process request: "+err.Error(), err)
		return
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to get contract ABI: "+err.Error(), err)
		return
	}

	data, err := abiData.Pack("addPieces", new(big.Int), recordKeeperAddr, pieceDataArray, extraDataBytes)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to pack method call: "+err.Error(), err)
		return
	}

	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to get sender address: "+err.Error(), err)
		return
	}

	tx := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPVerifier,
		contract.SybilFee(),
		0,
		nil,
		data,
	)

	reason := "pdp-create-and-add"
	txHash, err := p.sender.Send(workCtx, fromAddress, tx, reason)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to send transaction: "+err.Error(), err)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	txHashLower := strings.ToLower(txHash.Hex())
	log.Infow("PDP CreateDataSet: Inserting transaction tracking",
		"txHash", txHashLower,
		"service", serviceLabel,
		"recordKeeper", recordKeeperAddr.Hex())
	// Begin a database transaction
	comm, err := p.db.BeginTransaction(workCtx, func(tx *harmonydb.Tx) (bool, error) {
		err := p.insertMessageWaitsAndDataSetCreate(tx, txHashLower, serviceLabel)
		if err != nil {
			return false, err
		}
		// insert piece adds with data_set id = NULL as the dataset is pending
		err = p.insertPieceAdds(tx, nil, txHashLower, reqBody.Pieces, subPieceInfoMap)
		if err != nil {
			return false, err
		}

		// Enable indexing if the extraData indicates indexing is needed
		if mustIndex {
			log.Debugw("ExtraData metadata indicates indexing needed, marking all subpieces as needing indexing")
			subPieceRefIDs := make([]int64, 0, len(subPieceInfoMap))
			for _, info := range subPieceInfoMap {
				subPieceRefIDs = append(subPieceRefIDs, info.PDPPieceRefID)
			}
			if err := EnableIndexingForPiecesInTx(tx, serviceLabel, subPieceRefIDs); err != nil {
				return false, err
			}
		}

		return true, err
	}, harmonydb.OptionRetry())

	if err != nil {
		log.Errorf("Failed to insert into message_waits_eth, pdp_data_set_piece_adds and pdp_data_set_creates: %+v", err)
		httpServerError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	if !comm {
		log.Error("Failed to commit database transaction")
		httpServerError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	// Step 7: Respond with 201 Created and Location header
	w.Header().Set("Location", path.Join("/pdp/data-sets/created", txHashLower))
	w.WriteHeader(http.StatusCreated)
}

func decodeExtraData(extraDataString *string) ([]byte, error) {
	if extraDataString == nil {
		return nil, nil
	}

	extraDataHexStr := *extraDataString
	decodedBytes, err := hex.DecodeString(strings.TrimPrefix(extraDataHexStr, "0x"))
	if err != nil {
		log.Errorf("Failed to decode hex extraData: %v", err)
		return nil, err
	}
	return decodedBytes, nil
}

// handleCreateDataSet handles the creation of a new data set
func (p *PDPService) handleCreateDataSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	// Step 2: Parse the request body to get the 'recordKeeper' address and extraData
	type RequestBody struct {
		RecordKeeper string  `json:"recordKeeper"`
		ExtraData    *string `json:"extraData,omitempty"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Failed to read request body: "+err.Error(), err)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var reqBody RequestBody
	if err := json.Unmarshal(body, &reqBody); err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid JSON in request body: "+err.Error(), err)
		return
	}

	if reqBody.RecordKeeper == "" {
		httpServerError(w, http.StatusBadRequest, "recordKeeper address is required", err)
		return
	}

	recordKeeperAddr := common.HexToAddress(reqBody.RecordKeeper)
	if recordKeeperAddr == (common.Address{}) {
		httpServerError(w, http.StatusBadRequest, "Invalid recordKeeper address", err)
		return
	}

	// Check if the recordkeeper is in the whitelist for public services
	if contract.IsPublicService(serviceLabel) && !contract.IsRecordKeeperAllowed(recordKeeperAddr) {
		httpServerError(w, http.StatusForbidden, "recordKeeper address not allowed for public service", err)
		return
	}

	// Decode extraData if provided
	extraDataBytes, err := decodeExtraData(reqBody.ExtraData)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid extraData format (must be hex encoded): "+err.Error(), err)
		return
	}
	if len(extraDataBytes) > MaxCreateDataSetExtraDataSize {
		errMsg := fmt.Sprintf("extraData size (%d bytes) exceeds the maximum allowed limit for CreateDataSet (%d bytes)", len(extraDataBytes), MaxCreateDataSetExtraDataSize)
		httpServerError(w, http.StatusBadRequest, errMsg, err)
		return
	}

	// Step 3: Get the sender address from 'eth_keys' table where role = 'pdp' limit 1
	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to get sender address: "+err.Error(), err)
		return
	}

	// Step 4: Manually create the transaction without requiring a Signer
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to get contract ABI: "+err.Error(), err)
		return
	}

	// Pack the method call data
	data, err := abiData.Pack("createDataSet", recordKeeperAddr, extraDataBytes)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to pack method call: "+err.Error(), err)
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
	txHash, err := p.sender.Send(workCtx, fromAddress, tx, reason)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to send transaction: "+err.Error(), err)

		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Step 6: Insert into message_waits_eth and pdp_data_set_creates
	txHashLower := strings.ToLower(txHash.Hex())
	log.Infow("PDP CreateDataSet: Inserting transaction tracking",
		"txHash", txHashLower,
		"service", serviceLabel,
		"recordKeeper", recordKeeperAddr.Hex())

	// Begin a database transaction
	comm, err := p.db.BeginTransaction(workCtx, func(tx *harmonydb.Tx) (bool, error) {
		err := p.insertMessageWaitsAndDataSetCreate(tx, txHashLower, serviceLabel)
		if err != nil {
			return false, err
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		log.Errorf("Failed to insert database tracking records: %+v", err)
		httpServerError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	if !comm {
		log.Error("Failed to commit database transaction")
		httpServerError(w, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	// Step 7: Respond with 201 Created and Location header
	w.Header().Set("Location", path.Join("/pdp/data-sets/created", txHashLower))
	w.WriteHeader(http.StatusCreated)
}

// insertMessageWaitsAndDataSetCreate inserts records into message_waits_eth and pdp_data_set_creates
func (p *PDPService) insertMessageWaitsAndDataSetCreate(tx *harmonydb.Tx, txHashHex string, serviceLabel string) error {
	// Insert into message_waits_eth
	log.Debugw("Inserting into message_waits_eth",
		"txHash", txHashHex,
		"status", "pending")
	n, err := tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashHex, "pending")
	if err != nil {
		log.Errorw("Failed to insert into message_waits_eth",
			"txHash", txHashHex,
			"error", err)
		return err
	}

	if n != 1 {
		return fmt.Errorf("expected 1 row to be inserted into message_waits_eth, got %d", n)
	}

	// Insert into pdp_data_set_creates
	log.Debugw("Inserting into pdp_data_set_creates",
		"txHash", txHashHex,
		"service", serviceLabel)
	n, err = tx.Exec(`
            INSERT INTO pdp_data_set_creates (create_message_hash, service)
            VALUES ($1, $2)
        `, txHashHex, serviceLabel)
	if err != nil {
		log.Errorw("Failed to insert into pdp_data_set_creates",
			"txHash", txHashHex,
			"error", err)
		return err
	}

	if n != 1 {
		return fmt.Errorf("expected 1 row to be inserted into pdp_data_set_creates, got %d", n)
	}

	log.Infow("Successfully inserted transaction tracking records",
		"txHash", txHashHex,
		"service", serviceLabel)
	return nil
}
