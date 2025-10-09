package pdp

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract"
)

type SubPieceEntry struct {
	SubPieceCID   string `json:"subPieceCid"`
	subPieceCIDv1 string
}

type AddPieceRequest struct {
	PieceCID   string `json:"pieceCid"`
	pieceCIDv1 string
	SubPieces  []SubPieceEntry `json:"subPieces"`
}
type PieceData struct {
	Data []byte // CID
}

// Map to store subPieceCID -> [pieceInfo, pdp_pieceref.id, subPieceOffset]
type SubPieceInfo struct {
	PieceCIDv1     cid.Cid
	PaddedSize     abi.PaddedPieceSize
	RawSize        uint64 // RawSize is the size of the piece with no padding applied
	PDPPieceRefID  int64
	SubPieceOffset uint64
}

// returns PieceData, SubPieceInfo, and a list of subPieceCids
func (p *PDPService) transformAddPiecesRequest(ctx context.Context, serviceLabel string, pieces []AddPieceRequest) ([]PieceData, map[string]*SubPieceInfo, []string, error) {
	// Collect all subPieceCids to fetch their info in a batch
	subPieceCidSet := make(map[string]struct{})
	for _, addPieceReq := range pieces {
		if addPieceReq.PieceCID == "" {
			return nil, nil, nil, errors.New("PieceCID is required for each piece")
		}

		if len(addPieceReq.SubPieces) == 0 {
			return nil, nil, nil, errors.New("at least one subPiece is required per piece")
		}

		for i, subPieceEntry := range addPieceReq.SubPieces {
			if subPieceEntry.SubPieceCID == "" {
				return nil, nil, nil, errors.New("subPieceCid is required for each subPiece")
			}
			pieceCid, err := asPieceCIDv1(subPieceEntry.SubPieceCID)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("invalid SubPiece: %w", err)
			}
			pieceCidString := pieceCid.String()

			addPieceReq.SubPieces[i].subPieceCIDv1 = pieceCidString // save it for to query subPieceInfoMap later

			if _, exists := subPieceCidSet[pieceCidString]; exists {
				return nil, nil, nil, errors.New("duplicate subPieceCid in request")
			}

			subPieceCidSet[pieceCidString] = struct{}{}
		}
	}

	// Convert set to slice
	subPieceCidList := make([]string, 0, len(subPieceCidSet))
	for cidStr := range subPieceCidSet {
		subPieceCidList = append(subPieceCidList, cidStr)
	}

	subPieceInfoMap := make(map[string]*SubPieceInfo)

	// Start a DB transaction
	_, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
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
		for i, addPieceReq := range pieces {
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
			pieces[i].pieceCIDv1 = providedPieceCidv1.String()

			if !providedPieceCidv1.Equals(generatedPieceCid) {
				return false, fmt.Errorf("provided PieceCid does not match generated PieceCid: %s != %s", providedPieceCidv1, generatedPieceCid)
			}
		}

		// All validations passed, commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to validate subPieces: %w", err)
	}

	// Prepare PieceData array for Ethereum transaction
	// Define a Struct that matches the Solidity PieceData struct
	var pieceDataArray []PieceData

	for _, addPieceReq := range pieces {
		// Convert PieceCid to bytes
		pieceCidV2, err := cid.Decode(addPieceReq.PieceCID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid PieceCid: %w", err)
		}
		_, rawSize, err := commcid.PieceCidV1FromV2(pieceCidV2)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid CommPv2: %w", err)
		}
		height, _, err := commcid.PayloadSizeToV1TreeHeightAndPadding(rawSize)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("computing height and padding: %w", err)
		}
		if height > 50 {
			return nil, nil, nil, errors.New("invalid height")
		}

		// Get raw size by summing up the sizes of subPieces
		var totalSize uint64 = 0
		prevSubPieceSize := subPieceInfoMap[addPieceReq.SubPieces[0].subPieceCIDv1].PaddedSize
		for i, subPieceEntry := range addPieceReq.SubPieces {
			subPieceInfo := subPieceInfoMap[subPieceEntry.subPieceCIDv1]
			if subPieceInfo.PaddedSize > prevSubPieceSize {
				return nil, nil, nil, fmt.Errorf("subPieces must be in descending order of size, piece %d %s is larger than prev subPiece %s",
					i, subPieceEntry.SubPieceCID, addPieceReq.SubPieces[i-1].SubPieceCID)
			}

			prevSubPieceSize = subPieceInfo.PaddedSize
			totalSize += uint64(subPieceInfo.RawSize)
		}
		// sanity check that the rawSize in the CommPv2 matches the totalSize of the subPieces
		if rawSize != totalSize {
			return nil, nil, nil, fmt.Errorf("raw size miss-match: expected %d, got %d", totalSize, rawSize)
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
	return pieceDataArray, subPieceInfoMap, nil, nil
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

	// Step 4: Prepare piece information
	pieceDataArray, subPieceInfoMap, subPieceCidList, err := p.transformAddPiecesRequest(ctx, serviceLabel, payload.Pieces)
	if err != nil {
		http.Error(w, "Failed to transform add pieces request: "+err.Error(), http.StatusBadRequest)
	}

	// Step 5: Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 6: Prepare the Ethereum transaction
	// Pack the method call data
	// The extraDataBytes variable is now correctly populated above
	data, err := abiData.Pack("addPieces", dataSetId, common.Address{}, pieceDataArray, extraDataBytes)
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

	// Step 9: check for indexing requirements on data set.
	// Get listenerAddr from blockchain contract
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, p.ethClient)
	if err != nil {
		log.Errorw("Failed to instantiate PDPVerifier contract", "error", err, "dataSetId", dataSetId)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	listenerAddr, err := pdpVerifier.GetDataSetListener(nil, dataSetId)
	if err != nil {
		log.Errorw("Failed to get listener address for data set", "error", err, "dataSetId", dataSetId)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	mustIndex, _, err := contract.GetDataSetMetadataAtKey(listenerAddr, p.ethClient, dataSetId, "withIPFSIndexing")
	if err != nil {
		// Hard to differenctiate between unsupported listener type OR internal error
		// So we log on debug and skip indexing attempt
		mustIndex = false
		log.Infow("Failed to get data set metadata, skipping indexing ", "error", err, "dataSetId", dataSetId)
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
		err = p.insertPieceAdds(txdb, dataSetIdUint64, txHashLower, payload.Pieces, subPieceInfoMap)
		if err != nil {
			return false, err
		}

		if mustIndex {
			log.Debugw("Data set metadata exists, marking all subpieces as needing indexing", "dataSetId", dataSetId)
			// Note: it's possible to update a duplicate piece that has already completed the indexing step
			// but task_pdp_indexing handles pieces that have already been indexed smoothly
			_, err := txdb.Exec(`
				UPDATE pdp_piecerefs
				SET needs_indexing = TRUE
				WHERE service = $1
					AND piece_cid = ANY($2)
					AND needs_indexing = FALSE
				`, serviceLabel, subPieceCidList)
			if err != nil {
				return false, err
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

func (p *PDPService) insertPieceAdds(txdb *harmonydb.Tx, dataSetId uint64, txHash string, pieces []AddPieceRequest, subPieceInfoMap map[string]*SubPieceInfo) error {
	for addMessageIndex, addPieceReq := range pieces {
		for _, subPieceEntry := range addPieceReq.SubPieces {
			subPieceInfo := subPieceInfoMap[subPieceEntry.subPieceCIDv1]

			// Insert into pdp_data_set_pieces
			_, err := txdb.Exec(`
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
				dataSetId,
				addPieceReq.pieceCIDv1,
				txHash,
				addMessageIndex,
				subPieceEntry.subPieceCIDv1,
				subPieceInfo.SubPieceOffset,
				subPieceInfo.PaddedSize,
				subPieceInfo.PDPPieceRefID,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
