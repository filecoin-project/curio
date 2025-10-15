package pdp

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/pdp/contract"
)

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
