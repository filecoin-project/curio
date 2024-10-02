package pdp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"
	"io"
	"net/http"
	"os"
	"path"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PieceSizeLimit in bytes
var PieceSizeLimit = abi.PaddedPieceSize(256 << 20).Unpadded()

func (p *PDPService) handlePiecePost(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.verifyJWTToken(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		PieceCID string `json:"pieceCid"`
		RefID    string `json:"refId"`
		Notify   string `json:"notify,omitempty"`
	}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil || req.PieceCID == "" || req.RefID == "" {
		http.Error(w, "Invalid request body: missing pieceCid or refId", http.StatusBadRequest)
		return
	}

	// Check if the piece is already stored
	exists, err := p.PieceStore.HasPiece(req.PieceCID)
	if err != nil {
		http.Error(w, "Failed to check piece existence", http.StatusInternalServerError)
		return
	}

	if exists {
		// Return 204 No Content
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Create an upload UUID
	uploadUUID := uuid.New()

	// Store the upload request in the database
	ctx := r.Context()
	_, err = p.db.Exec(ctx, `
        INSERT INTO pdp_piece_uploads (id, service_id, piece_cid, notify_url) 
        VALUES (?, ?, ?, ?)
    `, uploadUUID.String(), serviceID, req.PieceCID, req.Notify)
	if err != nil {
		http.Error(w, "Failed to store upload request in database", http.StatusInternalServerError)
		return
	}

	// Create a location URL where the piece data can be uploaded via PUT
	uploadURL := path.Join(PDPRoutePath, "/piece/upload", uploadUUID.String())

	// Return 201 Created with Location header
	w.Header().Set("Location", uploadURL)
	w.WriteHeader(http.StatusCreated)
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
	var pieceCIDStr, notifyURL string
	var pieceRef sql.NullInt64
	err = p.db.QueryRow(ctx, `
        SELECT piece_cid, notify_url, piece_ref FROM pdp_piece_uploads WHERE id = $1
    `, uploadUUID.String()).Scan(&pieceCIDStr, &notifyURL, &pieceRef)
	if err != nil {
		if err == sql.ErrNoRows {
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

	// Limit the size of the piece data
	maxPieceSize := int64(PieceSizeLimit)

	// Create a commp.Calc instance for calculating commP
	cp := &commp.Calc{}
	readSize := int64(0)

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		defer f.Close()

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

	// Convert commP digest into a piece CID
	pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to convert commP to CID", http.StatusInternalServerError)
		return
	}

	// Compare the computed piece CID with the expected one from the database
	if pieceCIDComputed.String() != pieceCIDStr {
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

		// Change scheme to "stashstore"
		stashURL.Scheme = "stashstore"
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
            UPDATE pdp_piece_uploads SET piece_ref = $1 WHERE id = $2
        `, pieceRefID, uploadUUID.String())
		if err != nil {
			return false, fmt.Errorf("failed to update pdp_piece_uploads: %w", err)
		}

		return true, nil // Commit the transaction
	})

	if err != nil || !didCommit {
		// Remove the stash file as the transaction failed
		_ = p.storage.StashRemove(ctx, stashID)
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		return
	}

	// Optionally send a notification to the notify_url
	// TODO notify is called in
	/*if notifyURL != "" {
		// Prepare notification payload
		notificationPayload := map[string]string{
			"pieceCid": pieceCIDStr,
		}
		notificationData, err := json.Marshal(notificationPayload)
		if err != nil {
			// Log the error but proceed
			fmt.Println("Failed to marshal notification payload:", err)
		} else {
			// Send the notification (best effort)
			go func() {
				req, err := http.NewRequest("POST", notifyURL, bytes.NewReader(notificationData))
				if err != nil {
					// Log error
					fmt.Println("Failed to create notification request:", err)
					return
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					// Log error
					fmt.Println("Failed to send notification:", err)
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode >= 300 {
					// Log non-2xx responses
					fmt.Printf("Notification response status: %s\n", resp.Status)
				}
			}()
		}
	}*/

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}
