package pdp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logger "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	mhreg "github.com/multiformats/go-multihash/core"
	"github.com/snadrus/must"
	"github.com/yugabyte/pgx/v5"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

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

type PieceHash struct {
	// Name of the hash function used
	// sha2-256-trunc254-padded - CommP
	// sha2-256 - Blob sha256
	Name string `json:"name"`

	// hex encoded hash
	Hash string `json:"hash"`

	// Size of the piece in bytes
	Size int64 `json:"size"`
}

func (ph *PieceHash) Set() bool {
	return ph.Name != "" && ph.Hash != "" && ph.Size > 0
}

func (ph *PieceHash) mh() (multihash.Multihash, error) {
	_, ok := multihash.Names[ph.Name]
	if !ok {
		return nil, fmt.Errorf("hash function name not recognized: %s", ph.Name)
	}

	hashBytes, err := hex.DecodeString(ph.Hash)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hash: %w", err)
	}

	return multihash.EncodeName(hashBytes, ph.Name)
}

func (ph *PieceHash) commp(ctx context.Context, db *harmonydb.DB) (cid.Cid, bool, error) {
	// commp, known, error
	mh, err := ph.mh()
	if err != nil {
		return cid.Undef, false, fmt.Errorf("failed to decode hash: %w", err)
	}

	if ph.Name == multihash.Codes[multihash.SHA2_256_TRUNC254_PADDED] {
		return cid.NewCidV1(cid.FilCommitmentUnsealed, mh), true, nil
	}

	var commpStr string
	err = db.QueryRow(ctx, `
		SELECT commp FROM pdp_piece_mh_to_commp WHERE mhash = $1 AND size = $2
	`, mh, ph.Size).Scan(&commpStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return cid.Undef, false, nil
		}
		return cid.Undef, false, fmt.Errorf("failed to query pdp_piece_mh_to_commp: %w", err)
	}

	commpCid, err := cid.Parse(commpStr)
	if err != nil {
		return cid.Undef, false, fmt.Errorf("failed to parse commp CID: %w", err)
	}

	return commpCid, true, nil
}

func (ph *PieceHash) maybeStaticCommp() (cid.Cid, bool) {
	mh, err := ph.mh()
	if err != nil {
		return cid.Undef, false
	}

	if ph.Name == multihash.Codes[multihash.SHA2_256_TRUNC254_PADDED] {
		return cid.NewCidV1(cid.FilCommitmentUnsealed, mh), true
	}

	return cid.Undef, false
}

func (p *PDPService) handlePiecePost(w http.ResponseWriter, r *http.Request) {
	// Verify that the request is authorized using ECDSA JWT
	serviceID, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		Check  PieceHash `json:"check"`
		Notify string    `json:"notify,omitempty"`
	}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil || !req.Check.Set() {
		http.Error(w, "Invalid request body: missing pieceCid or refId", http.StatusBadRequest)
		return
	}

	if abi.UnpaddedPieceSize(req.Check.Size) > PieceSizeLimit {
		http.Error(w, "Piece size exceeds the maximum allowed size", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	pieceCid, havePieceCid, err := req.Check.commp(ctx, p.db)
	if err != nil {
		http.Error(w, "Failed to process request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Variables to hold information outside the transaction
	var uploadUUID uuid.UUID
	var uploadURL string
	var responseStatus int

	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if havePieceCid {
			// Check if a 'parked_pieces' entry exists for the given 'piece_cid'
			var parkedPieceID int64
			err := tx.QueryRow(`
            SELECT id FROM parked_pieces WHERE piece_cid = $1 AND long_term = TRUE AND complete = TRUE
        `, pieceCid).Scan(&parkedPieceID)
			if err != nil && err != pgx.ErrNoRows {
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
            `, uploadUUID.String(), serviceID, pieceCid, req.Notify, parkedPieceRefID, req.Check.Name, must.One(hex.DecodeString(req.Check.Hash)), req.Check.Size)
				if err != nil {
					return false, fmt.Errorf("failed to insert into pdp_piece_uploads: %w", err)
				}

				responseStatus = http.StatusOK
				return true, nil // Commit the transaction
			}
		}

		// Piece does not exist, proceed to create a new upload request
		uploadUUID = uuid.New()

		// Store the upload request in the database
		var pieceCidStr *string
		if p, ok := req.Check.maybeStaticCommp(); ok {
			ps := p.String()
			pieceCidStr = &ps
		}

		_, err = tx.Exec(`
            INSERT INTO pdp_piece_uploads (id, service, piece_cid, notify_url, check_hash_codec, check_hash, check_size)
            VALUES ($1, $2, $3, $4, $5, $6, $7)
        `, uploadUUID.String(), serviceID, pieceCidStr, req.Notify, req.Check.Name, must.One(hex.DecodeString(req.Check.Hash)), req.Check.Size)
		if err != nil {
			return false, fmt.Errorf("Failed to store upload request in database: %w", err)
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

	if responseStatus == http.StatusCreated {
		// Return 201 Created with Location header
		w.Header().Set("Location", uploadURL)
		w.WriteHeader(http.StatusCreated)
	} else if responseStatus == http.StatusOK {
		// Return 200 OK with the pieceCID
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"pieceCID": pieceCid.String()})
	} else {
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
	var notifyURL, checkHashName string
	var checkHash []byte
	var checkSize int64

	var pieceRef sql.NullInt64
	err = p.db.QueryRow(ctx, `
        SELECT piece_cid, notify_url, piece_ref, check_hash_codec, check_hash, check_size FROM pdp_piece_uploads WHERE id = $1
    `, uploadUUID.String()).Scan(&pieceCIDStr, &notifyURL, &pieceRef, &checkHashName, &checkHash, &checkSize)
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

	ph := PieceHash{
		Name: checkHashName,
		Hash: hex.EncodeToString(checkHash),
		Size: checkSize,
	}
	phMh, err := ph.mh()
	if err != nil {
		http.Error(w, "Failed to decode hash: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Limit the size of the piece data
	maxPieceSize := checkSize

	// Create a commp.Calc instance for calculating commP
	cp := &commp.Calc{}
	readSize := int64(0)

	var vhash hash.Hash
	if checkHashName != multihash.Codes[multihash.SHA2_256_TRUNC254_PADDED] {
		hasher, err := mhreg.GetVariableHasher(multihash.Names[checkHashName], -1)
		if err != nil {
			http.Error(w, "Failed to get hasher", http.StatusInternalServerError)
			return
		}
		vhash = hasher
	}

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitedReader := io.LimitReader(r.Body, maxPieceSize+1) // +1 to detect exceeding the limit
		multiWriter := io.MultiWriter(cp, f)
		if vhash != nil {
			multiWriter = io.MultiWriter(vhash, multiWriter)
		}

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

	var outHash = digest
	if vhash != nil {
		outHash = vhash.Sum(nil)
	}

	if !bytes.Equal(outHash, checkHash) {
		// Remove the stash file as the data is invalid
		_ = p.storage.StashRemove(ctx, stashID)
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

		if checkHashName != multihash.Codes[multihash.SHA2_256_TRUNC254_PADDED] {
			_, err = tx.Exec(`
				INSERT INTO pdp_piece_mh_to_commp (mhash, size, commp) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
			`, phMh, checkSize, pieceCIDComputed.String())
			if err != nil {
				return false, fmt.Errorf("failed to insert into pdp_piece_mh_to_commp: %w", err)
			}
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
	ctx := r.Context()
	startTime := time.Now()
	
	// Extract requester domain for metrics
	requesterDomain := extractDomainFromRequest(r.Host)
	
	// Create base context with tags for metrics
	ctx, err := tag.New(ctx,
		tag.Insert(RequesterDomainKey, requesterDomain),
	)
	if err != nil {
		log.Errorf("failed to create metrics context: %v", err)
	}
	
	// Record request count
	stats.Record(ctx, PDPRetrievalRequestCount.M(1))
	stats.Record(ctx, PDPDomainRequestCount.M(1))
	
	// Verify that the request is authorized using ECDSA JWT
	serviceLabel, err := p.AuthService(r)
	if err != nil {
		// Add service label and status to context
		ctx, _ = tag.New(ctx,
			tag.Insert(ServiceLabelKey, "unknown"),
			tag.Insert(ResponseStatusKey, "401"),
		)
		stats.Record(ctx, PDPRetrievalErrorCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	
	// Add service label to context
	ctx, err = tag.New(ctx, tag.Insert(ServiceLabelKey, serviceLabel))
	if err != nil {
		log.Errorf("failed to add service label to metrics context: %v", err)
	}
	
	// Record service-specific request
	stats.Record(ctx, PDPServiceRequestCount.M(1))

	// Parse query parameters
	sizeString := r.URL.Query().Get("size")
	size, err := strconv.ParseInt(sizeString, 10, 64)
	if err != nil {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "400"))
		stats.Record(ctx, PDPRetrievalErrorCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.Error(w, fmt.Sprintf("errors parsing size: %s", err.Error()), 400)
		return
	}
	req := PieceHash{
		Name: r.URL.Query().Get("name"),
		Hash: r.URL.Query().Get("hash"),
		Size: size,
	}

	pieceCid, havePieceCid, err := req.commp(ctx, p.db)
	if err != nil {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "500"))
		stats.Record(ctx, PDPRetrievalErrorCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.Error(w, "Failed to process request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// upload either not complete or does not exist
	if !havePieceCid {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "404"))
		stats.Record(ctx, PDPRetrievalNotFoundCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.NotFound(w, r)
		return
	}

	// Verify that a 'parked_pieces' entry exists for the given 'piece_cid'
	var count int
	err = p.db.QueryRow(ctx, `
    SELECT count(*) FROM parked_pieces WHERE piece_cid = $1 AND long_term = TRUE AND complete = TRUE
  `, pieceCid.String()).Scan(&count)
	if err != nil {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "500"))
		stats.Record(ctx, PDPRetrievalErrorCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if count == 0 {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "404"))
		stats.Record(ctx, PDPRetrievalNotFoundCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.NotFound(w, r)
		return
	}

	// Record piece size metrics with categorization
	pieceSizeCategory := categorizePieceSize(size)
	ctx, _ = tag.New(ctx, 
		tag.Insert(PieceSizeCategoryKey, pieceSizeCategory),
		tag.Insert(ResponseStatusKey, "200"),
	)
	stats.Record(ctx, PDPRetrievalPieceSize.M(size))
	stats.Record(ctx, PDPRetrievalSuccessCount.M(1))

	response := struct {
		PieceCID string `json:"piece_cid"`
	}{
		PieceCID: pieceCid.String(),
	}

	// encode response
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		ctx, _ = tag.New(ctx, tag.Insert(ResponseStatusKey, "500"))
		stats.Record(ctx, PDPRetrievalErrorCount.M(1))
		stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
		
		http.Error(w, "Failed to write response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Record successful request duration
	stats.Record(ctx, PDPRetrievalRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
}
