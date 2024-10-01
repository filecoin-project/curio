package pdp

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"
	"io"
	"net/http"
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

	// Lookup the expected pieceCID and notify_url from the database using uploadUUID
	var pieceCIDStr, notifyURL string
	err = p.db.QueryRow(ctx, `
        SELECT piece_cid, notify_url FROM pdp_piece_uploads WHERE id = $1
    `, uploadUUID.String()).Scan(&pieceCIDStr, &notifyURL)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Upload UUID not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	// Limit the size of the piece data
	maxPieceSize := int64(PieceSizeLimit)
	limitedReader := io.LimitReader(r.Body, maxPieceSize+1) // +1 to detect if it exceeds limit

	// Create a commp.Calc instance for calculating commP
	cp := &commp.Calc{}

	// Create an in-memory buffer to store the piece data
	var pieceBuffer bytes.Buffer

	// Use io.TeeReader to read from the limitedReader and write to both cp and pieceBuffer
	teeReader := io.TeeReader(limitedReader, &pieceBuffer)

	// Read data from teeReader and write to cp
	// We'll read in chunks to control memory usage
	buf := make([]byte, 32*1024) // 32 KiB buffer
	totalRead := int64(0)

	for {
		n, err := teeReader.Read(buf)
		if err != nil && err != io.EOF {
			http.Error(w, "Failed to read piece data", http.StatusInternalServerError)
			return
		}
		if n > 0 {
			// Write to commp.Calc
			_, err = cp.Write(buf[:n])
			if err != nil {
				http.Error(w, "Failed to calculate commP", http.StatusInternalServerError)
				return
			}
			totalRead += int64(n)
			if totalRead > maxPieceSize {
				http.Error(w, "Piece data exceeds the maximum allowed size", http.StatusRequestEntityTooLarge)
				return
			}
		}
		if err == io.EOF {
			break
		}
	}

	// Finalize the commP calculation
	digest, _, err := cp.Digest()
	if err != nil {
		http.Error(w, "Failed to finalize commP calculation", http.StatusInternalServerError)
		return
	}

	// Convert commP digest into a piece CID
	pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		http.Error(w, "Failed to convert commP to CID", http.StatusInternalServerError)
		return
	}

	// Compare the computed piece CID with the expected one from the database
	if pieceCIDComputed.String() != pieceCIDStr {
		http.Error(w, "Computed piece CID does not match expected piece CID", http.StatusBadRequest)
		return
	}

	// Store the piece data using PieceStore
	// Assuming PieceStore has a method StorePieceFromReader that accepts an io.Reader
	err = p.PieceStore.StorePieceFromReader(pieceCIDStr, &pieceBuffer)
	if err != nil {
		http.Error(w, "Failed to store piece", http.StatusInternalServerError)
		return
	}

	// Optionally send a notification to the notify_url
	if notifyURL != "" {
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
	}

	// Remove the upload record from pdp_piece_uploads as it's no longer needed
	_, err = p.db.Exec(ctx, `
        DELETE FROM pdp_piece_uploads WHERE id = $1
    `, uploadUUID.String())
	if err != nil {
		// Log the error but proceed
		fmt.Println("Failed to delete upload record:", err)
	}

	// Respond with 204 No Content
	w.WriteHeader(http.StatusNoContent)
}
