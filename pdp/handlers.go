package pdp

import (
	"context"
	"database/sql"
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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/go-commp-utils/nonffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	types2 "github.com/filecoin-project/lotus/chain/types"
)

// PDPRoutePath is the base path for PDP routes
const PDPRoutePath = "/pdp"

// PDPService represents the service for managing proof sets and pieces
type PDPService struct {
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
		db:      db,
		storage: stor,

		sender:    sn,
		ethClient: ec,
		filClient: fc,
	}
}

// Routes registers the HTTP routes with the provided router
func Routes(r *chi.Mux, p *PDPService) {

	// Routes for proof sets
	r.Route(path.Join(PDPRoutePath, "/proof-sets"), func(r chi.Router) {
		// POST /pdp/proof-sets - Create a new proof set
		r.Post("/", p.handleCreateProofSet)

		// GET /pdp/proof-sets/created/{txHash} - Get the status of a proof set creation
		r.Get("/created/{txHash}", p.handleGetProofSetCreationStatus)

		// Individual proof set routes
		r.Route("/{proofSetID}", func(r chi.Router) {
			// GET /pdp/proof-sets/{set-id}
			r.Get("/", p.handleGetProofSet)

			// DEL /pdp/proof-sets/{set-id}
			r.Delete("/", p.handleDeleteProofSet)

			// Routes for roots within a proof set
			r.Route("/roots", func(r chi.Router) {
				// POST /pdp/proof-sets/{set-id}/roots
				r.Post("/", p.handleAddRootToProofSet)

				// Individual root routes
				r.Route("/{rootID}", func(r chi.Router) {
					// GET /pdp/proof-sets/{set-id}/roots/{root-id}
					r.Get("/", p.handleGetProofSetRoot)

					// DEL /pdp/proof-sets/{set-id}/roots/{root-id}
					r.Delete("/", p.handleDeleteProofSetRoot)
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
	_, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Return 200 OK
	w.WriteHeader(http.StatusOK)
}

// handleCreateProofSet handles the creation of a new proof set
func (p *PDPService) handleCreateProofSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Parse the request body to get the 'recordKeeper' address
	type RequestBody struct {
		RecordKeeper string `json:"recordKeeper"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

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
	data, err := abiData.Pack("createProofSet", recordKeeperAddr, []byte{})
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
	reason := "pdp-mkproofset"
	txHash, err := p.sender.Send(ctx, fromAddress, tx, reason)
	if err != nil {
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Step 6: Insert into message_waits_eth and pdp_proofset_creates
	err = p.insertMessageWaitsAndProofsetCreate(ctx, txHash.Hex(), serviceLabel)
	if err != nil {
		log.Errorf("Failed to insert into message_waits_eth and pdp_proofset_creates: %+v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Step 7: Respond with 201 Created and Location header
	w.Header().Set("Location", path.Join("/pdp/proof-sets/created", txHash.Hex()))
	w.WriteHeader(http.StatusCreated)
}

// getSenderAddress retrieves the sender address from the database where role = 'pdp' limit 1
func (p *PDPService) getSenderAddress(ctx context.Context) (common.Address, error) {
	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
}

// insertMessageWaitsAndProofsetCreate inserts records into message_waits_eth and pdp_proofset_creates
func (p *PDPService) insertMessageWaitsAndProofsetCreate(ctx context.Context, txHashHex string, serviceLabel string) error {
	// Begin a database transaction
	_, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err := tx.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHashHex, "pending")
		if err != nil {
			return false, err // Return false to rollback the transaction
		}

		// Insert into pdp_proofset_creates
		_, err = tx.Exec(`
            INSERT INTO pdp_proofset_creates (create_message_hash, service)
            VALUES ($1, $2)
        `, txHashHex, serviceLabel)
		if err != nil {
			return false, err // Return false to rollback the transaction
		}

		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	return nil
}

// handleGetProofSetCreationStatus handles the GET request to retrieve the status of a proof set creation
func (p *PDPService) handleGetProofSetCreationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.verifyJWTToken(r)
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

	// Validate txHash is a valid hash
	if len(txHash) != 66 { // '0x' + 64 hex chars
		http.Error(w, "Invalid txHash length", http.StatusBadRequest)
		return
	}
	if _, err := hex.DecodeString(txHash[2:]); err != nil {
		http.Error(w, "Invalid txHash format", http.StatusBadRequest)
		return
	}

	// Step 3: Lookup pdp_proofset_creates by create_message_hash (which is txHash)
	var proofSetCreate struct {
		CreateMessageHash string `db:"create_message_hash"`
		OK                *bool  `db:"ok"` // Pointer to handle NULL
		ProofSetCreated   bool   `db:"proofset_created"`
		Service           string `db:"service"`
	}

	err = p.db.QueryRow(ctx, `
        SELECT create_message_hash, ok, proofset_created, service
        FROM pdp_proofset_creates
        WHERE create_message_hash = $1
    `, txHash).Scan(&proofSetCreate.CreateMessageHash, &proofSetCreate.OK, &proofSetCreate.ProofSetCreated, &proofSetCreate.Service)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Proof set creation not found for given txHash", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to query proof set creation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 4: Check that the service matches the requesting service
	if proofSetCreate.Service != serviceLabel {
		http.Error(w, "Unauthorized: service label mismatch", http.StatusUnauthorized)
		return
	}

	// Step 5: Prepare the response
	response := struct {
		CreateMessageHash string  `json:"createMessageHash"`
		ProofsetCreated   bool    `json:"proofsetCreated"`
		Service           string  `json:"service"`
		TxStatus          string  `json:"txStatus"`
		OK                *bool   `json:"ok"`
		ProofSetId        *uint64 `json:"proofSetId,omitempty"`
	}{
		CreateMessageHash: proofSetCreate.CreateMessageHash,
		ProofsetCreated:   proofSetCreate.ProofSetCreated,
		Service:           proofSetCreate.Service,
		OK:                proofSetCreate.OK,
	}

	// Now get the tx_status from message_waits_eth
	var txStatus string
	err = p.db.QueryRow(ctx, `
        SELECT tx_status
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, txHash).Scan(&txStatus)
	if err != nil {
		if err == sql.ErrNoRows {
			// This should not happen as per foreign key constraints
			http.Error(w, "Message status not found for given txHash", http.StatusInternalServerError)
			return
		}
		http.Error(w, "Failed to query message status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response.TxStatus = txStatus

	if proofSetCreate.ProofSetCreated {
		// The proof set has been created, get the proofSetId from pdp_proof_sets
		var proofSetId uint64
		err = p.db.QueryRow(ctx, `
            SELECT id
            FROM pdp_proof_sets
            WHERE create_message_hash = $1
        `, txHash).Scan(&proofSetId)
		if err != nil {
			if err == sql.ErrNoRows {
				// Should not happen, but handle gracefully
				http.Error(w, "Proof set not found despite proofset_created = true", http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to query proof set: "+err.Error(), http.StatusInternalServerError)
			return
		}
		response.ProofSetId = &proofSetId
	}

	// Step 6: Return the response as JSON
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleGetProofSet handles the GET request to retrieve the details of a proof set
func (p *PDPService) handleGetProofSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract proofSetId from the URL
	proofSetIdStr := chi.URLParam(r, "proofSetID")
	if proofSetIdStr == "" {
		http.Error(w, "Missing proof set ID in URL", http.StatusBadRequest)
		return
	}

	// Convert proofSetId to uint64
	proofSetId, err := strconv.ParseUint(proofSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID format", http.StatusBadRequest)
		return
	}

	// Step 3: Retrieve the proof set from the database
	var proofSet struct {
		ID      uint64 `db:"id"`
		Service string `db:"service"`
	}

	err = p.db.QueryRow(ctx, `
        SELECT id, service
        FROM pdp_proof_sets
        WHERE id = $1
    `, proofSetId).Scan(&proofSet.ID, &proofSet.Service)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Proof set not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to retrieve proof set: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 4: Check that the proof set belongs to the requesting service
	if proofSet.Service != serviceLabel {
		http.Error(w, "Unauthorized: proof set does not belong to your service", http.StatusUnauthorized)
		return
	}

	// Step 5: Retrieve the roots associated with the proof set
	var roots []struct {
		RootID        uint64 `db:"root_id"`
		RootCID       string `db:"root"`
		SubrootCID    string `db:"subroot"`
		SubrootOffset int64  `db:"subroot_offset"`
	}

	err = p.db.Select(ctx, &roots, `
        SELECT root_id, root, subroot, subroot_offset
        FROM pdp_proofset_roots
        WHERE proofset = $1
        ORDER BY root_id, subroot_offset
    `, proofSetId)
	if err != nil {
		http.Error(w, "Failed to retrieve proof set roots: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 6: Get the next challenge epoch
	var nextChallengeEpoch int64
	err = p.db.QueryRow(ctx, `
        SELECT prove_at_epoch
        FROM pdp_proof_sets
        WHERE id = $1
    `, proofSetId).Scan(&nextChallengeEpoch)
	if err != nil {
		http.Error(w, "Failed to retrieve next challenge epoch: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Step 6: Prepare the response
	response := struct {
		ID                 uint64      `json:"id"`
		Roots              []RootEntry `json:"roots"`
		NextChallengeEpoch int64       `json:"nextChallengeEpoch"`
	}{
		ID:                 proofSet.ID,
		NextChallengeEpoch: nextChallengeEpoch,
	}

	// Convert roots to the desired JSON format
	for _, root := range roots {
		response.Roots = append(response.Roots, RootEntry{
			RootID:        root.RootID,
			RootCID:       root.RootCID,
			SubrootCID:    root.SubrootCID,
			SubrootOffset: root.SubrootOffset,
		})
	}

	// Step 7: Return the response as JSON
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// RootEntry represents a root in the proof set for JSON serialization
type RootEntry struct {
	RootID        uint64 `json:"rootId"`
	RootCID       string `json:"rootCid"`
	SubrootCID    string `json:"subrootCid"`
	SubrootOffset int64  `json:"subrootOffset"`
}

func (p *PDPService) handleDeleteProofSet(w http.ResponseWriter, r *http.Request) {
	// ### DEL /proof-sets/{set id}
	// Remove the specified proof set entirely

	http.Error(w, "todo", http.StatusBadRequest)
}

func (p *PDPService) handleAddRootToProofSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract proofSetID from the URL
	proofSetIDStr := chi.URLParam(r, "proofSetID")
	if proofSetIDStr == "" {
		http.Error(w, "Missing proof set ID in URL", http.StatusBadRequest)
		return
	}

	// Convert proofSetID to uint64
	proofSetIDUint64, err := strconv.ParseUint(proofSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID format", http.StatusBadRequest)
		return
	}

	// check if the proofset belongs to the service in pdp_proof_sets

	var proofSetService string
	err = p.db.QueryRow(ctx, `
			SELECT service 
			FROM pdp_proof_sets
			WHERE id = $1
		`, proofSetIDUint64).Scan(&proofSetService)
	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "Proof set not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to retrieve proof set: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if proofSetService != serviceLabel {
		// same as when actually not found to avoid leaking information in obvious ways
		http.Error(w, "Proof set not found", http.StatusNotFound)
		return
	}

	// Convert proofSetID to *big.Int
	proofSetID := new(big.Int).SetUint64(proofSetIDUint64)

	// Step 3: Parse the request body
	type SubrootEntry struct {
		SubrootCID string `json:"subrootCid"`
	}

	type AddRootRequest struct {
		RootCID  string         `json:"rootCid"`
		Subroots []SubrootEntry `json:"subroots"`
	}

	var req []AddRootRequest // array because we can add multiple roots in one request
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(req) == 0 {
		http.Error(w, "At least one root must be provided", http.StatusBadRequest)
		return
	}

	// Collect all subrootCIDs to fetch their info in a batch
	subrootCIDsSet := make(map[string]struct{})
	for _, addRootReq := range req {
		if addRootReq.RootCID == "" {
			http.Error(w, "RootCID is required for each root", http.StatusBadRequest)
			return
		}

		if len(addRootReq.Subroots) == 0 {
			http.Error(w, "At least one subroot is required per root", http.StatusBadRequest)
			return
		}

		for _, subrootEntry := range addRootReq.Subroots {
			if subrootEntry.SubrootCID == "" {
				http.Error(w, "subrootCid is required for each subroot", http.StatusBadRequest)
				return
			}
			if _, exists := subrootCIDsSet[subrootEntry.SubrootCID]; exists {
				http.Error(w, "duplicate subrootCid in request", http.StatusBadRequest)
				return
			}

			subrootCIDsSet[subrootEntry.SubrootCID] = struct{}{}
		}
	}

	// Convert set to slice
	subrootCIDsList := make([]string, 0, len(subrootCIDsSet))
	for cidStr := range subrootCIDsSet {
		subrootCIDsList = append(subrootCIDsList, cidStr)
	}

	// Map to store subrootCID -> [pieceInfo, pdp_pieceref.id, subrootOffset]
	type SubrootInfo struct {
		PieceInfo     abi.PieceInfo
		PDPPieceRefID int64
		SubrootOffset uint64
	}

	subrootInfoMap := make(map[string]*SubrootInfo)

	// Start a DB transaction
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Step 4: Get pdp_piecerefs matching all subroot cids + make sure those refs belong to serviceLabel
		rows, err := tx.Query(`
            SELECT ppr.piece_cid, ppr.id AS pdp_pieceref_id, ppr.piece_ref,
                   pp.piece_padded_size
            FROM pdp_piecerefs ppr
            JOIN parked_piece_refs pprf ON pprf.ref_id = ppr.piece_ref
            JOIN parked_pieces pp ON pp.id = pprf.piece_id
            WHERE ppr.service = $1 AND ppr.piece_cid = ANY($2)
        `, serviceLabel, subrootCIDsList)
		if err != nil {
			return false, err
		}
		defer rows.Close()

		foundSubroots := make(map[string]struct{})
		for rows.Next() {
			var pieceCIDStr string
			var pdpPieceRefID, pieceRefID int64
			var piecePaddedSize uint64

			err := rows.Scan(&pieceCIDStr, &pdpPieceRefID, &pieceRefID, &piecePaddedSize)
			if err != nil {
				return false, err
			}

			// Parse the piece CID
			pieceCID, err := cid.Decode(pieceCIDStr)
			if err != nil {
				return false, fmt.Errorf("invalid piece CID in database: %s", pieceCIDStr)
			}

			// Create PieceInfo
			pieceInfo := abi.PieceInfo{
				Size:     abi.PaddedPieceSize(piecePaddedSize),
				PieceCID: pieceCID,
			}

			subrootInfoMap[pieceCIDStr] = &SubrootInfo{
				PieceInfo:     pieceInfo,
				PDPPieceRefID: pdpPieceRefID,
				SubrootOffset: 0, // Will compute offset later
			}

			foundSubroots[pieceCIDStr] = struct{}{}
		}

		// Check if all subroot CIDs were found
		for _, cidStr := range subrootCIDsList {
			if _, found := foundSubroots[cidStr]; !found {
				return false, fmt.Errorf("subroot CID %s not found or does not belong to service %s", cidStr, serviceLabel)
			}
		}

		// Now, for each AddRootRequest, validate RootCID and prepare data for ETH transaction
		for _, addRootReq := range req {
			// Collect pieceInfos for subroots
			pieceInfos := make([]abi.PieceInfo, len(addRootReq.Subroots))

			var totalOffset uint64 = 0
			for i, subrootEntry := range addRootReq.Subroots {
				subrootInfo, exists := subrootInfoMap[subrootEntry.SubrootCID]
				if !exists {
					return false, fmt.Errorf("subroot CID %s not found in subroot info map", subrootEntry.SubrootCID)
				}

				// Update SubrootOffset
				subrootInfo.SubrootOffset = totalOffset
				subrootInfoMap[subrootEntry.SubrootCID] = subrootInfo // Update the map

				pieceInfos[i] = subrootInfo.PieceInfo

				totalOffset += uint64(subrootInfo.PieceInfo.Size)
			}

			// Use GenerateUnsealedCID to generate RootCID from subroots
			proofType := abi.RegisteredSealProof_StackedDrg64GiBV1_1 // Proof type sets max piece size, nothing else
			generatedRootCID, err := nonffi.GenerateUnsealedCID(proofType, pieceInfos)
			if err != nil {
				return false, fmt.Errorf("failed to generate RootCID: %v", err)
			}

			// Compare generated RootCID with provided RootCID
			providedRootCID, err := cid.Decode(addRootReq.RootCID)
			if err != nil {
				return false, fmt.Errorf("invalid provided RootCID: %v", err)
			}

			if !providedRootCID.Equals(generatedRootCID) {
				return false, fmt.Errorf("provided RootCID does not match generated RootCID: %s != %s", providedRootCID, generatedRootCID)
			}
		}

		// All validations passed, commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		http.Error(w, "Failed to validate subroots: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Step 5: Prepare the Ethereum transaction data outside the DB transaction
	// Obtain the ABI of the PDPVerifier contract
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepare RootData array for Ethereum transaction
	// Define a Struct that matches the Solidity RootData struct
	type RootData struct {
		Root    struct{ Data []byte }
		RawSize *big.Int
	}

	var rootDataArray []RootData

	for _, addRootReq := range req {
		// Convert RootCID to bytes
		rootCID, err := cid.Decode(addRootReq.RootCID)
		if err != nil {
			http.Error(w, "Invalid RootCID: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Get raw size by summing up the sizes of subroots
		var totalSize uint64 = 0
		var prevSubrootSize = subrootInfoMap[addRootReq.Subroots[0].SubrootCID].PieceInfo.Size
		for i, subrootEntry := range addRootReq.Subroots {
			subrootInfo := subrootInfoMap[subrootEntry.SubrootCID]
			if subrootInfo.PieceInfo.Size > prevSubrootSize {
				msg := fmt.Sprintf("Subroots must be in descending order of size, root %d %s is larger than prev subroot %s", i, subrootEntry.SubrootCID, addRootReq.Subroots[i-1].SubrootCID)
				http.Error(w, msg, http.StatusBadRequest)
				return
			}

			prevSubrootSize = subrootInfo.PieceInfo.Size
			totalSize += uint64(subrootInfo.PieceInfo.Size.Unpadded())
		}

		// Prepare RootData for Ethereum transaction
		rootData := RootData{
			Root:    struct{ Data []byte }{Data: rootCID.Bytes()},
			RawSize: new(big.Int).SetUint64(totalSize),
		}

		rootDataArray = append(rootDataArray, rootData)
	}

	// Step 6: Prepare the Ethereum transaction
	// Pack the method call data
	data, err := abiData.Pack("addRoots", proofSetID, rootDataArray, []byte{})
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
	reason := "pdp-addroots"
	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Step 9: Insert into message_waits_eth and pdp_proofset_roots
	_, err = p.db.BeginTransaction(ctx, func(txdb *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err := txdb.Exec(`
            INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
            VALUES ($1, $2)
        `, txHash.Hex(), "pending")
		if err != nil {
			return false, err // Return false to rollback the transaction
		}

		// Update proof set for initialization upon first add
		_, err = txdb.Exec(`
			UPDATE pdp_proof_sets SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
			`, proofSetIDUint64)
		if err != nil {
			return false, err
		}

		// Insert into pdp_proofset_roots

		for addMessageIndex, addRootReq := range req {
			for _, subrootEntry := range addRootReq.Subroots {
				subrootInfo := subrootInfoMap[subrootEntry.SubrootCID]

				// Insert into pdp_proofset_roots
				_, err = txdb.Exec(`
                    INSERT INTO pdp_proofset_root_adds (
                        proofset,
                        root,
                        add_message_hash,
                        add_message_index,
                        subroot,
                        subroot_offset,
						subroot_size,
                        pdp_pieceref
                    )
                    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
                `,
					proofSetIDUint64,
					addRootReq.RootCID,
					txHash.Hex(),
					addMessageIndex,
					subrootEntry.SubrootCID,
					subrootInfo.SubrootOffset,
					subrootInfo.PieceInfo.Size,
					subrootInfo.PDPPieceRefID,
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
		log.Errorw("Failed to insert into database", "error", err, "txHash", txHash.Hex(), "subroots", subrootInfoMap)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Step 10: Respond with 201 Created
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handleDeleteProofSetRoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Step 1: Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Step 2: Extract parameters from the URL
	proofSetIdStr := chi.URLParam(r, "proofSetID")
	if proofSetIdStr == "" {
		http.Error(w, "Missing proof set ID in URL", http.StatusBadRequest)
		return
	}
	rootIdStr := chi.URLParam(r, "rootID")
	if rootIdStr == "" {
		http.Error(w, "Missing root ID in URL", http.StatusBadRequest)
		return
	}

	// Convert proofSetId to uint64
	proofSetID, err := strconv.ParseUint(proofSetIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid proof set ID format", http.StatusBadRequest)
		return
	}
	rootID, err := strconv.ParseUint(rootIdStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid root ID format", http.StatusBadRequest)
		return
	}

	// check if the proofset belongs to the service in pdp_proof_sets
	var proofSetService string
	err = p.db.QueryRow(ctx, `
			SELECT service 
			FROM pdp_proof_sets
			WHERE id = $1
		`, proofSetID).Scan(&proofSetService)
	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "Proof set not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to retrieve proof set: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if proofSetService != serviceLabel {
		// same as when actually not found to avoid leaking information in obvious ways
		http.Error(w, "Proof set not found", http.StatusNotFound)
		return
	}

	// Get the ABI and pack the transaction data
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		http.Error(w, "Failed to get contract ABI: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Pack the method call data
	data, err := abiData.Pack("scheduleRemovals",
		big.NewInt(int64(proofSetID)),
		[]*big.Int{big.NewInt(int64(rootID))},
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
	reason := "pdp-delete-root"
	txHash, err := p.sender.Send(ctx, fromAddress, ethTx, reason)
	if err != nil {
		http.Error(w, "Failed to send transaction: "+err.Error(), http.StatusInternalServerError)
		log.Errorf("Failed to send transaction: %+v", err)
		return
	}

	// Schedule deletion of the root from the proof set using a transaction
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits_eth
		_, err := tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, $2)
		`, txHash.Hex(), "pending")
		if err != nil {
			return false, err
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		if err.Error() == "root not found" {
			http.Error(w, "Root not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to schedule delete root: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return 204 No Content on successful deletion
	w.WriteHeader(http.StatusNoContent)

}

func (p *PDPService) handleGetProofSetRoot(w http.ResponseWriter, r *http.Request) {
	// Spec snippet:
	// ### GET /proof-sets/{set id}/roots/{root id}
	// Response Body:
	// {
	//   "rootId": {root ID},
	//   "rootCid": "bafy....root",
	//   "subroots": [
	//     {
	//       "subrootCid": "bafy...subroot",
	//       "subrootOffset": 0,
	//       "pieceCid": "bafy...piece1"
	//     },
	//     //...
	//   ]
	// }

	/*	proofSetIDStr := chi.URLParam(r, "proofSetID")
		proofSetID, err := strconv.ParseInt(proofSetIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid proof set ID", http.StatusBadRequest)
			return
		}

		rootIDStr := chi.URLParam(r, "rootID")
		rootID, err := strconv.ParseInt(rootIDStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid root ID", http.StatusBadRequest)
			return
		}*/

	// Retrieve root from proof set in store
	/*rootDetails, err := p.ProofSetStore.GetProofSetRoot(proofSetID, rootID)
	if err != nil {
		http.Error(w, "Root not found", http.StatusNotFound)
		return
	}*/

	// Respond with root details
	w.Header().Set("Content-Type", "application/json")
	/*err = json.NewEncoder(w).Encode(rootDetails)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}*/
}

// Data models corresponding to the updated schema

// PDPOwnerAddress represents the owner address with its private key
type PDPOwnerAddress struct {
	OwnerAddress string // PRIMARY KEY
	PrivateKey   []byte // BYTEA NOT NULL
}

// PDPServiceEntry represents a PDP service entry
type PDPServiceEntry struct {
	ID           int64     // PRIMARY KEY
	PublicKey    []byte    // BYTEA NOT NULL
	ServiceLabel string    // TEXT NOT NULL
	CreatedAt    time.Time // DEFAULT CURRENT_TIMESTAMP
}

// PDPPieceRef represents a PDP piece reference
type PDPPieceRef struct {
	ID         int64     // PRIMARY KEY
	ServiceID  int64     // pdp_services.id
	PieceCID   string    // TEXT NOT NULL
	RefID      string    // TEXT NOT NULL
	ServiceTag string    // VARCHAR(64)
	ClientTag  string    // VARCHAR(64)
	CreatedAt  time.Time // DEFAULT CURRENT_TIMESTAMP
}

// PDPProofSet represents a proof set
type PDPProofSet struct {
	ID                 int64 // PRIMARY KEY (on-chain proofset id)
	NextChallengeEpoch int64 // Cached chain value
}

// PDPProofSetRoot represents a root in a proof set
type PDPProofSetRoot struct {
	ProofSetID    int64  // proofset BIGINT NOT NULL
	RootID        int64  // root_id BIGINT NOT NULL
	Root          string // root TEXT NOT NULL
	Subroot       string // subroot TEXT NOT NULL
	SubrootOffset int64  // subroot_offset BIGINT NOT NULL
	PDPPieceRefID int64  // pdp_piecerefs.id
}

// PDPProveTask represents a prove task
type PDPProveTask struct {
	ProofSetID     int64  // proofset
	ChallengeEpoch int64  // challenge epoch
	TaskID         int64  // harmonytask task ID
	MessageCID     string // text
	MessageEthHash string // text
}

// Interfaces

// ProofSetStore defines methods to manage proof sets and roots
type ProofSetStore interface {
	CreateProofSet(proofSet *PDPProofSet) (int64, error)
	GetProofSet(proofSetID int64) (*PDPProofSetDetails, error)
	DeleteProofSet(proofSetID int64) error
	AddProofSetRoot(proofSetRoot *PDPProofSetRoot) error
	DeleteProofSetRoot(proofSetID int64, rootID int64) error
}

// PieceStore defines methods to manage pieces and piece references
type PieceStore interface {
	HasPiece(pieceCID string) (bool, error)
	StorePiece(pieceCID string, data []byte) error
	GetPiece(pieceCID string) ([]byte, error)
	GetPieceRefIDByPieceCID(pieceCID string) (int64, error)
}

// OwnerAddressStore defines methods to manage owner addresses
type OwnerAddressStore interface {
	HasOwnerAddress(ownerAddress string) (bool, error)
}

// PDPProofSetDetails represents the details of a proof set, including roots
type PDPProofSetDetails struct {
	ID                 int64                   `json:"id"`
	NextChallengeEpoch int64                   `json:"nextChallengeEpoch"`
	Roots              []PDPProofSetRootDetail `json:"roots"`
}

// PDPProofSetRootDetail represents the details of a root in a proof set
type PDPProofSetRootDetail struct {
	RootID   int64                      `json:"rootId"`
	RootCID  string                     `json:"rootCid"`
	Subroots []PDPProofSetSubrootDetail `json:"subroots"`
}

// PDPProofSetSubrootDetail represents a subroot in a proof set root
type PDPProofSetSubrootDetail struct {
	SubrootCID    string `json:"subrootCid"`
	SubrootOffset int64  `json:"subrootOffset"`
	PieceCID      string `json:"pieceCid"`
}
