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
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		PieceCID string `json:"pieceCid"`
		Notify   string `json:"notify,omitempty"`
	}
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	pieceCidV2, size, err := asPieceCIDv2(req.PieceCID, 0)
	if err != nil {
		http.Error(w, "Invalid request body: invalid pieceCid: "+err.Error(), http.StatusBadRequest)
		return
	}
	if size > uint64(PieceSizeLimit) {
		http.Error(w, "Piece size exceeds the maximum allowed size", http.StatusBadRequest)
		return
	}
	pieceCidV1, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		http.Error(w, "Invalid request body: invalid pieceCid", http.StatusBadRequest)
		return
	}

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

		if err == nil {
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

			// Create a new 'pdp_piece_uploads' entry pointing to the 'parked_piece_refs' entry
			uploadUUID = uuid.New()
			_, err = tx.Exec(`
                INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, piece_ref, check_hash_codec, check_hash, check_size)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            `, uploadUUID.String(), serviceID, pieceCidV1.String(), req.Notify, parkedPieceRefID, multicodec.Sha2_256Trunc254Padded.String(), dmh.Digest, size)
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_piece_uploads: %w", err)
			}

			responseStatus = http.StatusOK
			return true, nil // Commit the transaction
		}

		// Piece does not exist, proceed to create a new upload request
		uploadUUID = uuid.New()

		_, err = tx.Exec(`
       INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, check_hash_codec, check_hash, check_size)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
   `, uploadUUID.String(), serviceID, pieceCidV1.String(), req.Notify, multicodec.Sha2_256Trunc254Padded.String(), dmh.Digest, size)
		if err != nil {
			return false, fmt.Errorf("failed to store upload request in database: %w", err)
		}

		// Create a location URL where the piece data can be uploaded via PUT
		uploadURL = path.Join(PDPRoutePath, "/piece/upload", uploadUUID.String())
		responseStatus = http.StatusCreated

		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())
	if err != nil {
		http.Error(w, "Failed to process request: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
		http.Error(w, "Unexpected error", http.StatusInternalServerError)
	}
}

// handlePieceUpload handles the PUT request to upload the actual bytes of the piece
func (p *PDPService) handlePieceUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract the uploadUUID from the URL
	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		http.Error(w, "Invalid upload UUID", http.StatusBadRequest)
		return
	}

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
			http.Error(w, "Upload UUID not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	// Check that piece_ref is null; non-null means data was already uploaded
	if pieceRef.Valid {
		http.Error(w, "Data has already been uploaded", http.StatusConflict)
		return
	}

	pieceCidV1, err := cid.Parse(*pieceCIDStr)
	if err != nil {
		http.Error(w, "Failed to convert piece CID (v1): "+err.Error(), http.StatusInternalServerError)
		return
	}
	dmh, err := multihash.Decode(pieceCidV1.Hash())
	if err != nil {
		http.Error(w, "Failed to decode piece CID: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Limit the size of the piece data
	maxPieceSize := checkSize

	// Create a commp.Calc instance for calculating commP
	cp := &commp.Calc{}
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
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			http.Error(w, "Failed to store piece data", http.StatusInternalServerError)
			return
		}
	}

	// Finalize the commP calculation
	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to finalize commP calculation", http.StatusInternalServerError)
		return
	}

	if readSize != checkSize {
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Piece size does not match the expected size", http.StatusBadRequest)
		return
	}

	outHash := digest

	if !bytes.Equal(outHash, dmh.Digest) {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		log.Warnw("Computed hash does not match expected hash", "computed", hex.EncodeToString(outHash), "expected", hex.EncodeToString(dmh.Digest), "pieceCid", pieceCidV1.String())
		http.Error(w, "Computed hash does not match expected hash", http.StatusBadRequest)
		return
	}

	// Convert commP digest into a piece CID
	pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to convert commP to CID", http.StatusInternalServerError)
		return
	}

	// Compare the computed piece CID with the expected one from the database
	if pieceCIDStr != nil && pieceCIDComputed.String() != *pieceCIDStr {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Computed piece CID does not match expected piece CID", http.StatusBadRequest)
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

		// 3. Update the pdp_piece_uploads entry to contain the created piece_ref
		_, err = tx.Exec(`
            UPDATE pdp_piece_uploads SET piece_ref = $1, piece_cid = $2 WHERE id = $3
        `, pieceRefID, pieceCIDComputed.String(), uploadUUID.String())
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_piece_uploads: %w", err)
		}

		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())

	if err != nil || !didCommit {
		// Remove the stash file as the transaction failed
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		return
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// handle find piece allows one to look up a pdp piece by its original post data as
// query parameters
func (p *PDPService) handleFindPiece(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	_, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse query parameters

	cidStr := r.URL.Query().Get("pieceCid")
	pieceCidV2, _, err := asPieceCIDv2(cidStr, 0)
	if err != nil {
		http.Error(w, "Failed to parse CID: "+err.Error(), http.StatusBadRequest)
		return
	}
	pieceCidV1, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		http.Error(w, "Failed to get piece CID v1: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// Verify that a 'parked_pieces' entry exists for the given 'piece_cid'
	var exist bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1) AS exist;`, pieceCidV1.String()).Scan(&exist)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !exist {
		http.NotFound(w, r)
		return
	}

	response := struct {
		PieceCID string `json:"pieceCid"`
	}{
		PieceCID: pieceCidV2.String(),
	}

	// encode response
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (p *PDPService) handleStreamingUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	uploadUUID := uuid.New()
	uploadURL := path.Join(PDPRoutePath, "/piece/uploads", uploadUUID.String())

	n, err := p.db.Exec(r.Context(), `INSERT INTO pdp_piece_streaming_uploads (id, service) VALUES ($1, $2)`, uploadUUID.String(), serviceID)
	if err != nil {
		log.Errorw("Failed to create upload request in database", "error", err)
		http.Error(w, "Failed to create upload request", http.StatusInternalServerError)
		return
	}
	if n != 1 {
		log.Errorf("Failed to create upload request in database: expected 1 row but got %d", n)
		http.Error(w, "Failed to create upload request", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", uploadURL)
	w.WriteHeader(http.StatusCreated)
}

func (p *PDPService) handleStreamingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		http.Error(w, "Invalid upload UUID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var exists bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2)`, uploadUUID.String(), serviceID).Scan(&exists)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	reader := NewTimeoutLimitReader(r.Body, 5*time.Second)
	cp := &commp.Calc{}
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
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			http.Error(w, "Failed to store piece data", http.StatusInternalServerError)
			return
		}
	}

	// Finalize the commP calculation
	digest, paddedPieceSize, err := cp.Digest()
	if err != nil {
		log.Errorw("Failed to finalize commP calculation", "error", err)
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to finalize commP calculation", http.StatusInternalServerError)
		return
	}

	pcid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		log.Errorw("Failed to calculate PieceCIDV2", "error", err)
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to calculate PieceCIDV2", http.StatusInternalServerError)
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
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		return
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)

}

func (p *PDPService) handleFinalizeStreamingUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	uploadUUIDStr := chi.URLParam(r, "uploadUUID")
	uploadUUID, err := uuid.Parse(uploadUUIDStr)
	if err != nil {
		http.Error(w, "Invalid upload UUID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var exists bool
	err = p.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2)`, uploadUUID.String(), serviceID).Scan(&exists)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
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
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	pcid, err := cid.Parse(req.PieceCID)
	if err != nil {
		http.Error(w, "Invalid request body: invalid pieceCid", http.StatusBadRequest)
		return
	}

	digest, err := commcid.CIDToDataCommitmentV1(pcid)
	if err != nil {
		http.Error(w, "Invalid request body: invalid pieceCid", http.StatusBadRequest)
		return
	}

	var dPcidStr string
	var pSize, pref int64

	err = p.db.QueryRow(ctx, `SELECT piece_cid, piece_size, piece_ref FROM pdp_piece_streaming_uploads WHERE id = $1 AND service = $2 AND complete = TRUE`, uploadUUID.String(), serviceID).Scan(&dPcidStr, &pSize)
	if err != nil {
		log.Errorw("Failed to query pdp_piece_streaming_uploads", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	dPcid, err := cid.Parse(dPcidStr)
	if err != nil {
		log.Errorw("Failed to parse pieceCid", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if !pcid.Equals(dPcid) {
		http.Error(w, "Invalid request body: pieceCid does not match the calculated pieceCid for the uploaded piece", http.StatusBadRequest)
		return
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`
       INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, check_hash_codec, check_hash, check_size, piece_ref)
       VALUES ($1, $2, $3, $4, $5, $6, $7)
   `, uploadUUID.String(), serviceID, pcid.String(), req.Notify, multicodec.Sha2_256Trunc254Padded.String(), digest, pSize, pref)
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
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		return
	}
	if !comm {
		log.Errorw("Failed to process piece upload", "error", "failed to commit transaction")
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
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
