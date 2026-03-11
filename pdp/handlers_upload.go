package pdp

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logger "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/proof"
)

var log = logger.Logger("pdp")

// PieceSizeLimit in bytes
var PieceSizeLimit = abi.PaddedPieceSize(proof.MaxMemtreeSize).Unpadded()

func (p *PDPService) handlePiecePost(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	// Parse request body
	var req struct {
		PieceCID string `json:"pieceCid"`
		Notify   string `json:"notify,omitempty"`
	}
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: "+err.Error(), err)
		return
	}
	pieceInfo, err := ParsePieceCidV2(req.PieceCID)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: invalid pieceCid: "+err.Error(), err)
		return
	}
	if pieceInfo.RawSize > uint64(PieceSizeLimit) {
		httpServerError(w, http.StatusBadRequest, "Piece size exceeds the maximum allowed size", err)
		return
	}
	pieceCidV1 := pieceInfo.CidV1
	pieceCidV2 := pieceInfo.CidV2
	size := pieceInfo.RawSize
	log.Debugw("[handlePiecePost] -- piece stuff done", "pieceCidV2", pieceCidV2)

	ctx := r.Context()

	// Variables to hold information outside the transaction
	var uploadUUID uuid.UUID
	var uploadURL string
	var responseStatus int

	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		dmh, err := multihash.Decode(pieceCidV1.Hash())
		if err != nil {
			return false, fmt.Errorf("failed to decode multihash: %w", err)
		}

		// Check if a 'parked_pieces' entry exists for the given 'piece_cid'
		var parkedPieceID int64
		err = tx.QueryRow(`
            SELECT id FROM parked_pieces WHERE piece_cid = $1 AND long_term = TRUE AND complete = TRUE
        `, pieceCidV1.String()).Scan(&parkedPieceID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("failed to query parked_pieces: %w", err)
		}
		log.Debugw("[handlePiecePost] -- parked piece check done", "pieceCidV2", pieceCidV2)
		if err == nil {
			log.Debugw("[handlePiecePost] -- parked piece found", "pieceCidV2", pieceCidV2)
			// Piece is already stored
			// Create a new 'parked_piece_refs' entry
			var parkedPieceRefID int64
			err = tx.QueryRow(`
                INSERT INTO parked_piece_refs (piece_id, long_term)
                VALUES ($1, TRUE) RETURNING ref_id
            `, parkedPieceID).Scan(&parkedPieceRefID)
			if err != nil {
				return false, fmt.Errorf("failed to insert into parked_piece_refs: %w", err)
			}
			log.Debugw("[handlePiecePost] -- new parked piece ref", "parkedPieceRefID", parkedPieceRefID, "pieceCidV1", pieceCidV1)

			// Create a new 'pdp_piece_uploads' entry pointing to the 'parked_piece_refs' entry
			uploadUUID = uuid.New()
			_, err = tx.Exec(`
                INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, piece_ref, check_hash_codec, check_hash, check_size)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            `, uploadUUID.String(), serviceID, pieceCidV1.String(), req.Notify, parkedPieceRefID, multicodec.Sha2_256Trunc254Padded.String(), dmh.Digest, size)
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_piece_uploads: %w", err)
			}
			log.Debugw("[handlePiecePost] -- new pdp_piece_uploads", "uploadUUID", uploadUUID, "pieceCidV1", pieceCidV1)

			responseStatus = http.StatusOK
			return true, nil // Commit the transaction
		}
		log.Debugw("[handlePiecePost] -- parked piece not found", "pieceCidV2", pieceCidV2)

		// Piece does not exist, proceed to create a new upload request
		uploadUUID = uuid.New()

		_, err = tx.Exec(`
       INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, check_hash_codec, check_hash, check_size)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
   `, uploadUUID.String(), serviceID, pieceCidV1.String(), req.Notify, multicodec.Sha2_256Trunc254Padded.String(), dmh.Digest, size)
		if err != nil {
			return false, fmt.Errorf("failed to store upload request in database: %w", err)
		}
		log.Debugw("[handlePiecePost] -- new pdp_piece_uploads inserted", "uploadUUID", uploadUUID, "pieceCidV2", pieceCidV2)

		// Create a location URL where the piece data can be uploaded via PUT
		uploadURL = path.Join(PDPRoutePath, "/piece/upload", uploadUUID.String())
		responseStatus = http.StatusCreated

		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to process request: "+err.Error(), err)
		return
	}
	log.Debugw("[handlePiecePost] -- writing response", "uploadUUID", uploadUUID, "pieceCidV2", pieceCidV2)

	switch responseStatus {
	case http.StatusCreated:
		// Return 201 Created with Location header
		w.Header().Set("Location", uploadURL)
		w.WriteHeader(http.StatusCreated)
	case http.StatusOK:
		// Return 200 OK with the pieceCID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"pieceCid": pieceCidV2.String()})
	default:
		// Should not reach here
		httpServerError(w, http.StatusInternalServerError, "Unexpected error", err)
	}
}

// handlePieceUpload handles the PUT request to upload the actual bytes of the piece
func (p *PDPService) handlePieceUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpServerError(w, http.StatusMethodNotAllowed, "Method Not Allowed", nil)
		return
	}

	// Extract the uploadUUID from the URL
	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid upload UUID", err)
		return
	}
	log.Debugw("[handlePieceUpload] -- upload started", "uploadUUID", uploadUUID)
	ctx := r.Context()

	// Lookup the expected pieceCID, notify_url, and piece_ref from the database using uploadUUID
	var pieceCIDStr *string
	var notifyURL string
	var checkSize int64

	var pieceRef sql.NullInt64
	err = p.db.QueryRow(ctx, `
        SELECT piece_cid, notify_url, piece_ref, check_size FROM pdp_piece_uploads WHERE id = $1
    `, uploadUUID.String()).Scan(&pieceCIDStr, &notifyURL, &pieceRef, &checkSize)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpServerError(w, http.StatusNotFound, "Upload UUID not found", err)
		} else {
			httpServerError(w, http.StatusInternalServerError, "Database error", err)
		}
		return
	}
	log.Debugw("[handlePieceUpload] -- upload lookup done", "uploadUUID", uploadUUID)
	// Check that piece_ref is null; non-null means data was already uploaded
	if pieceRef.Valid {
		httpServerError(w, http.StatusConflict, "Data has already been uploaded", err)
		return
	}

	pieceCidV1, err := cid.Parse(*pieceCIDStr)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to convert piece CID (v1): "+err.Error(), err)
		return
	}
	dmh, err := multihash.Decode(pieceCidV1.Hash())
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to decode piece CID: "+err.Error(), err)
		return
	}

	// Limit the size of the piece data
	maxPieceSize := checkSize

	// Create a commp.Calc instance for calculating commP
	cp := &commp.Calc{}
	defer cp.Reset()
	readSize := int64(0)

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitedReader := io.LimitReader(r.Body, maxPieceSize+1) // +1 to detect exceeding the limit
		multiWriter := io.MultiWriter(cp, f)

		// Copy data from limitedReader to multiWriter
		n, err := io.Copy(multiWriter, limitedReader)
		if err != nil {
			return fmt.Errorf("failed to read and write piece data: %w", err)
		}

		if n > maxPieceSize {
			return fmt.Errorf("piece data exceeds the maximum allowed size")
		}

		readSize = n

		return nil
	}
	// Upload into StashStore
	stashID, err := p.storage.StashCreate(ctx, maxPieceSize, writeFunc)
	if err != nil {
		if err.Error() == "piece data exceeds the maximum allowed size" {
			httpServerError(w, http.StatusRequestEntityTooLarge, err.Error(), err)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			httpServerError(w, http.StatusInternalServerError, "Failed to store piece data", err)
			return
		}
	}
	log.Debugw("[handlePieceUpload] -- uploaded into StashStore", "uploadUUID", uploadUUID)

	// Finalize the commP calculation
	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to finalize commP calculation", err)
		return
	}

	if readSize != checkSize {
		log.Debugw("[handlePieceUpload] -- piece size does not match the expected size removing from stash store", "uploadUUID", uploadUUID)
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusBadRequest, "Piece size does not match the expected size", err)
		return
	}

	outHash := digest

	if !bytes.Equal(outHash, dmh.Digest) {
		log.Debugw("[handlePieceUpload] -- computed hash does not match expected hash removing from stash store", "uploadUUID", uploadUUID)
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		log.Warnw("Computed hash does not match expected hash", "computed", hex.EncodeToString(outHash), "expected", hex.EncodeToString(dmh.Digest), "pieceCid", pieceCidV1.String())
		httpServerError(w, http.StatusBadRequest, "Computed hash does not match expected hash", err)
		return
	}

	// Convert commP digest into a piece CID
	pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to convert commP to CID", err)
		return
	}

	// Compare the computed piece CID with the expected one from the database
	if pieceCIDStr != nil && pieceCIDComputed.String() != *pieceCIDStr {
		log.Debugw("[handlePieceUpload] -- computed piece CID does not match expected piece CID removing from stash store", "uploadUUID", uploadUUID)
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusBadRequest, "Computed piece CID does not match expected piece CID", err)
		return
	}

	didCommit, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// 1. Create a long-term parked piece entry
		var parkedPieceID int64
		err := tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
            VALUES ($1, $2, $3, TRUE) RETURNING id
        `, pieceCIDComputed.String(), paddedPieceSize, readSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}
		log.Debugw("[handlePieceUpload] -- parked pieces entry created", "uploadUUID", uploadUUID)
		// 2. Create a piece ref with data_url being "stashstore://<stash-url>"
		// Get StashURL
		stashURL, err := p.storage.StashURL(stashID)
		if err != nil {
			return false, fmt.Errorf("failed to get stash URL: %w", err)
		}

		// Change scheme to "custore"
		stashURL.Scheme = dealdata.CustoreScheme
		dataURL := stashURL.String()

		var pieceRefID int64
		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, dataURL).Scan(&pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}
		log.Debugw("[handlePieceUpload] -- parked piece ref created", "uploadUUID", uploadUUID)
		// 3. Update the pdp_piece_uploads entry to contain the created piece_ref
		_, err = tx.Exec(`
            UPDATE pdp_piece_uploads SET piece_ref = $1, piece_cid = $2 WHERE id = $3
        `, pieceRefID, pieceCIDComputed.String(), uploadUUID.String())
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_piece_uploads: %w", err)
		}
		log.Debugw("[handlePieceUpload] -- pdp_piece_uploads entry updated", "uploadUUID", uploadUUID)
		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())

	if err != nil || !didCommit {
		// Remove the stash file as the transaction failed
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to process piece upload", err)
		return
	}

	log.Debugw("[handlePieceUpload] -- piece upload done, writing response", "uploadUUID", uploadUUID)
	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// handle find piece allows one to look up a pdp piece by its original post data as
// query parameters
func (p *PDPService) handleFindPiece(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	_, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	// Parse query parameters

	cidStr := r.URL.Query().Get("pieceCid")
	pieceInfo, err := ParsePieceCidV2(cidStr)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Failed to parse CID: "+err.Error(), err)
		return
	}
	pieceCidV1 := pieceInfo.CidV1

	ctx := r.Context()

	// Verify that a 'parked_pieces' entry exists for the given 'piece_cid'
	var exist bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1) AS exist;`, pieceCidV1.String()).Scan(&exist)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Database error", err)
		return
	}
	if !exist {
		http.NotFound(w, r)
		return
	}

	response := struct {
		PieceCID string `json:"pieceCid"`
	}{
		PieceCID: pieceInfo.CidV2.String(),
	}

	// encode response
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to write response: "+err.Error(), err)
		return
	}
}

func (p *PDPService) handleStreamingUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpServerError(w, http.StatusMethodNotAllowed, "Method Not Allowed", nil)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	uploadUUID := uuid.New()
	uploadURL := path.Join(PDPRoutePath, "/piece/uploads", uploadUUID.String())

	n, err := p.db.Exec(r.Context(), `INSERT INTO pdp_piece_streaming_uploads (id, service) VALUES ($1, $2)`, uploadUUID.String(), serviceID)
	if err != nil {
		log.Errorw("Failed to create upload request in database", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Failed to create upload request", err)
		return
	}
	if n != 1 {
		log.Errorf("Failed to create upload request in database: expected 1 row but got %d", n)
		httpServerError(w, http.StatusInternalServerError, "Failed to create upload request", err)
		return
	}

	w.Header().Set("Location", uploadURL)
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handleStreamingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		httpServerError(w, http.StatusMethodNotAllowed, "Method Not Allowed", nil)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid upload UUID", err)
		return
	}

	ctx := r.Context()

	var exists bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2)`, uploadUUID.String(), serviceID).Scan(&exists)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Database error", err)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	reader := NewTimeoutLimitReader(r.Body, 5*time.Second)
	cp := &commp.Calc{}
	defer cp.Reset()
	readSize := int64(0)

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		multiWriter := io.MultiWriter(cp, f)

		// Copy data from limitedReader to multiWriter
		n, err := io.Copy(multiWriter, reader)
		if err != nil {
			return fmt.Errorf("failed to read and write piece data: %w", err)
		}

		if n > UploadSizeLimit {
			return fmt.Errorf("piece data exceeds the maximum allowed size")
		}

		readSize = n

		return nil
	}

	// Upload into StashStore
	stashID, err := p.storage.StashCreate(ctx, UploadSizeLimit, writeFunc)
	if err != nil {
		if err.Error() == "piece data exceeds the maximum allowed size" {
			httpServerError(w, http.StatusRequestEntityTooLarge, err.Error(), err)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			httpServerError(w, http.StatusInternalServerError, "Failed to store piece data", err)
			return
		}
	}

	// Finalize the commP calculation
	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		log.Errorw("Failed to finalize commP calculation", "error", err)
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to finalize commP calculation", err)
		return
	}

	pcid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		log.Errorw("Failed to calculate PieceCIDV2", "error", err)
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to calculate PieceCIDV2", err)
		return
	}

	didCommit, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// 1. Create a long-term parked piece entry
		var parkedPieceID int64
		err := tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
            VALUES ($1, $2, $3, TRUE) RETURNING id
        `, pcid.String(), paddedPieceSize, readSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		// 2. Create a piece ref with data_url being "stashstore://<stash-url>"
		// Get StashURL
		stashURL, err := p.storage.StashURL(stashID)
		if err != nil {
			return false, fmt.Errorf("failed to get stash URL: %w", err)
		}

		// Change scheme to "custore"
		stashURL.Scheme = dealdata.CustoreScheme
		dataURL := stashURL.String()

		var pieceRefID int64
		err = tx.QueryRow(`
            INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
            VALUES ($1, $2, TRUE) RETURNING ref_id
        `, parkedPieceID, dataURL).Scan(&pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
		}

		// 3. Update the pdp_piece_streaming_uploads entry
		_, err = tx.Exec(`
            UPDATE pdp_piece_streaming_uploads SET piece_ref = $1, piece_cid = $2, piece_size = $3, raw_size = $4, complete = TRUE, completed_at = NOW() AT TIME ZONE 'UTC' WHERE id = $5 and service = $6
        `, pieceRefID, pcid.String(), paddedPieceSize, readSize, uploadUUID.String(), serviceID)
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_piece_streaming_uploads: %w", err)
		}

		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())

	if err != nil || !didCommit {
		// Remove the stash file as the transaction failed
		if err != nil {
			log.Errorw("Failed to process piece upload", "error", err)
		} else {
			log.Errorw("Failed to process piece upload", "error", "failed to commit transaction")
		}
		_ = p.storage.StashRemove(ctx, stashID)
		httpServerError(w, http.StatusInternalServerError, "Failed to process piece upload", err)
		return
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (p *PDPService) handleFinalizeStreamingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpServerError(w, http.StatusMethodNotAllowed, "Method Not Allowed", nil)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid upload UUID", err)
		return
	}

	ctx := r.Context()

	var exists bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2)`, uploadUUID.String(), serviceID).Scan(&exists)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Database error", err)
		return
	}

	if !exists {
		http.NotFound(w, r)
		return
	}

	var req struct {
		PieceCID string `json:"pieceCid"`
		Notify   string `json:"notify,omitempty"`
	}
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: "+err.Error(), err)
		return
	}

	// Parse PieceCID v2 from API (strictly requires v2 format)
	pieceInfo, err := ParsePieceCidV2(req.PieceCID)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: invalid pieceCid: "+err.Error(), err)
		return
	}
	pieceCidV1 := pieceInfo.CidV1

	// Get digest for insertion
	digest, err := commcid.CIDToDataCommitmentV1(pieceCidV1)
	if err != nil {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: invalid pieceCid", err)
		return
	}

	// Query database for stored piece info
	var dPcidStr string
	var pref int64
	var rawSize uint64

	err = p.db.QueryRow(ctx, `SELECT piece_cid, piece_ref, raw_size FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2 AND complete = TRUE`, uploadUUID.String(), serviceID).Scan(&dPcidStr, &pref, &rawSize)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Database error", err)
		return
	}

	// Validate size matches (prevents attack with smaller tree)
	if pieceInfo.RawSize != rawSize {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: pieceCid size does not match uploaded piece size", err)
		return
	}

	// Parse database PieceCID (v1 format)
	dPcid, err := cid.Parse(dPcidStr)
	if err != nil {
		log.Errorw("Failed to parse pieceCid", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Database error", err)
		return
	}

	// Compare v1 CIDs (database stores v1)
	if !pieceCidV1.Equals(dPcid) {
		httpServerError(w, http.StatusBadRequest, "Invalid request body: pieceCid does not match the calculated pieceCid for the uploaded piece", err)
		return
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`
       INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, check_hash_codec, check_hash, check_size, piece_ref)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
   `, uploadUUID.String(), serviceID, pieceCidV1.String(), req.Notify, multicodec.Sha2_256Trunc254Padded.String(), digest, pieceInfo.RawSize, pref)
		if err != nil {
			return false, fmt.Errorf("failed to store upload request in database: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("failed to store upload request in database: expected 1 row but got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2 AND complete = TRUE`, uploadUUID.String(), serviceID)
		if err != nil {
			return false, fmt.Errorf("failed to delete pdp_piece_streaming_uploads entry: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("Failed to process piece upload", "error", err)
		httpServerError(w, http.StatusInternalServerError, "Failed to process piece upload", err)
		return
	}
	if !comm {
		log.Errorw("Failed to process piece upload", "error", "failed to commit transaction")
		httpServerError(w, http.StatusInternalServerError, "Failed to process piece upload", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type TimeoutLimitReader struct {
	r          io.Reader
	timeout    time.Duration
	totalBytes int64
}

func NewTimeoutLimitReader(r io.Reader, timeout time.Duration) *TimeoutLimitReader {
	return &TimeoutLimitReader{
		r:          r,
		timeout:    timeout,
		totalBytes: 0,
	}
}

const UploadSizeLimit = int64(1065353216) // 1 GiB.Unpadded()

func (t *TimeoutLimitReader) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.timeout)
	for {
		// Attempt to read
		n, err := t.r.Read(p)
		if t.totalBytes+int64(n) > UploadSizeLimit {
			return 0, fmt.Errorf("upload size limit exceeded: %d bytes", UploadSizeLimit)
		} else {
			t.totalBytes += int64(n)
		}

		if err != nil {
			return n, err
		}

		if n > 0 {
			// Otherwise return byte read and no error
			return n, err
		}

		// Timeout: If we hit the deadline without making progress, return a timeout error
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("upload timeout: no progress (duration: %f Seconds)", t.timeout.Seconds())
		}

		// Avoid tight loop by adding a tiny sleep
		time.Sleep(100 * time.Millisecond) // Small pause to avoid busy-waiting
	}
}
