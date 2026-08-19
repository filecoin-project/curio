package pdp

import (
	"context"
	"database/sql"
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
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/parkpiece"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
)

var log = logging.Logger("pdpv0")

var (
	// 127
	PieceSizeMinLimit = abi.PaddedPieceSize(128).Unpadded()
	// 1065353216
	PieceSizeMaxLimit = abi.PaddedPieceSize(proof.MaxMemtreeSize).Unpadded()
)
var (
	ErrPieceTooSmall       = fmt.Errorf("piece data is below the minimum allowed size (%d bytes)", PieceSizeMinLimit)
	ErrPieceTooLarge       = fmt.Errorf("piece data exceeds the maximum allowed size (%d bytes)", PieceSizeMaxLimit)
	ErrExceedsDeclaredSize = fmt.Errorf("piece data exceeds the declared piece size")
	errPieceTooShort       = errors.New("piece data is shorter than the declared piece size")
	errUploadInProgress    = errors.New("another upload is already writing this piece")
	errUploadClaimed       = errors.New("upload UUID has already been claimed")
)

const minPaddedPieceSizeForCache = int64(32 * 1024 * 1024)

type exactSizeReader struct {
	r         io.Reader
	remaining int64
}

func (r *exactSizeReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}

	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) && r.remaining > 0 {
		return n, errPieceTooShort
	}
	return n, err
}

func readHasExtraByte(r io.Reader) (bool, error) {
	var buf [1]byte
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			return true, nil
		}
		if err != nil {
			// TimeoutLimitReader reports an over-limit byte as an error rather
			// than returning it. It still proves that the body exceeded the
			// exact size declared by PieceCIDv2.
			if errors.Is(err, ErrPieceTooLarge) {
				return true, nil
			}
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
	}
}

func needsSaveCache(rawSize int64) bool {
	return PadPieceSize(rawSize) >= minPaddedPieceSizeForCache
}

func insertPDPReference(tx *harmonydb.Tx, service, pieceCID string, pieceRef, rawSize int64) error {
	n, err := tx.Exec(`
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ($1, $2, $3, NOW(), $4)
	`, service, pieceCID, pieceRef, needsSaveCache(rawSize))
	if err != nil {
		return fmt.Errorf("failed to insert pdp_piecerefs: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("failed to insert pdp_piecerefs: expected 1 row, got %d", n)
	}
	return nil
}

func deleteClaimedUpload(tx *harmonydb.Tx, uploadID string, pieceRef int64) error {
	n, err := tx.Exec(`DELETE FROM pdp_piece_uploads WHERE id = $1 AND piece_ref = $2`, uploadID, pieceRef)
	if err != nil {
		return fmt.Errorf("failed to delete pdp_piece_uploads row: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("failed to delete pdp_piece_uploads row: expected 1 row, got %d", n)
	}
	return nil
}

type directUploadClaim struct {
	parkedPieceID int64
	pieceRefID    int64
	created       bool
	complete      bool
}

// claimDirectUpload atomically claims an unclaimed upload intent. For a new
// piece, it binds the intent to a skip=true parked-piece ref so this handler
// owns the direct write. If the piece became complete after POST, it publishes
// the PDP ref and consumes the intent without reading the request body.
func (p *PDPService) claimDirectUpload(ctx context.Context, uploadID, service, pieceCID string, rawSize, paddedSize int64) (directUploadClaim, error) {
	var claim directUploadClaim
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		claim = directUploadClaim{}
		claim.parkedPieceID, claim.created, err = parkpiece.UpsertSkipWithInserted(tx, pieceCID, paddedSize, rawSize, true, true)
		if err != nil {
			return false, fmt.Errorf("failed to claim parked piece: %w", err)
		}

		if !claim.created {
			err = tx.QueryRow(`SELECT complete FROM parked_pieces WHERE id = $1`, claim.parkedPieceID).Scan(&claim.complete)
			if err != nil {
				return false, fmt.Errorf("failed to inspect existing parked piece: %w", err)
			}
			if !claim.complete {
				return false, errUploadInProgress
			}
		}

		err = tx.QueryRow(`
			INSERT INTO parked_piece_refs (piece_id, long_term)
			VALUES ($1, TRUE)
			RETURNING ref_id
		`, claim.parkedPieceID).Scan(&claim.pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked piece ref: %w", err)
		}

		if claim.complete {
			n, err := tx.Exec(`DELETE FROM pdp_piece_uploads WHERE id = $1 AND piece_ref IS NULL`, uploadID)
			if err != nil {
				return false, fmt.Errorf("failed to consume upload UUID: %w", err)
			}
			if n != 1 {
				return false, errUploadClaimed
			}
			if err := insertPDPReference(tx, service, pieceCID, claim.pieceRefID, rawSize); err != nil {
				return false, err
			}
			return true, nil
		}

		n, err := tx.Exec(`
			UPDATE pdp_piece_uploads
			SET piece_ref = $1, piece_cid = $2, created_at = NOW()
			WHERE id = $3 AND piece_ref IS NULL
		`, claim.pieceRefID, pieceCID, uploadID)
		if err != nil {
			return false, fmt.Errorf("failed to claim upload UUID: %w", err)
		}
		if n != 1 {
			return false, errUploadClaimed
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return directUploadClaim{}, err
	}
	if !committed {
		return directUploadClaim{}, errors.New("failed to commit direct upload claim")
	}
	return claim, nil
}

func (p *PDPService) finalizeDirectUpload(ctx context.Context, uploadID, service, pieceCID string, claim directUploadClaim, rawSize int64) error {
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE parked_pieces
			SET complete = TRUE
			WHERE id = $1 AND complete = FALSE AND skip = TRUE AND cleanup_task_id IS NULL
		`, claim.parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to mark parked piece complete: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("failed to mark parked piece complete: expected 1 row, got %d", n)
		}

		if err := insertPDPReference(tx, service, pieceCID, claim.pieceRefID, rawSize); err != nil {
			return false, err
		}
		if err := deleteClaimedUpload(tx, uploadID, claim.pieceRefID); err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("failed to commit direct upload completion")
	}
	return nil
}

// releaseDirectUploadClaim makes a failed or expired upload retryable. It only
// removes the parked row when no other subsystem attached a ref to it.
func (p *PDPService) releaseDirectUploadClaim(ctx context.Context, uploadID string, pieceRefID, parkedPieceID int64) error {
	removePiece := false
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		removePiece = false
		n, err := tx.Exec(`
			UPDATE pdp_piece_uploads
			SET piece_ref = NULL, created_at = NOW()
			WHERE id = $1 AND piece_ref = $2
		`, uploadID, pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to release upload claim: %w", err)
		}
		if n == 0 {
			var finalized bool
			err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pdp_piecerefs WHERE piece_ref = $1)`, pieceRefID).Scan(&finalized)
			if err != nil {
				return false, fmt.Errorf("failed to check direct upload finalization: %w", err)
			}
			if finalized {
				return true, nil
			}
		}
		if n > 1 {
			return false, fmt.Errorf("failed to release upload claim: expected 1 row, got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1 AND piece_id = $2`, pieceRefID, parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to delete direct upload ref: %w", err)
		}

		n, err = tx.Exec(`
			DELETE FROM parked_pieces pp
			WHERE pp.id = $1
			  AND pp.complete = FALSE
			  AND pp.skip = TRUE
			  AND NOT EXISTS (
				  SELECT 1 FROM parked_piece_refs ppr WHERE ppr.piece_id = pp.id
			  )
		`, parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to delete abandoned parked piece: %w", err)
		}
		removePiece = n == 1

		// Pull or another subsystem may have attached a usable source while the
		// direct upload was running. Let StorePiece take over in that case.
		if !removePiece {
			_, err = tx.Exec(`
			UPDATE parked_pieces pp
			SET skip = FALSE
			WHERE pp.id = $1
			  AND pp.complete = FALSE
			  AND pp.skip = TRUE
			  AND EXISTS (
				  SELECT 1 FROM parked_piece_refs ppr
				  WHERE ppr.piece_id = pp.id AND ppr.data_url IS NOT NULL
			  )
			`, parkedPieceID)
			if err != nil {
				return false, fmt.Errorf("failed to release parked piece to StorePiece: %w", err)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("failed to commit direct upload claim release")
	}
	if removePiece {
		if err := p.pieceIO.RemovePiece(ctx, storiface.PieceNumber(parkedPieceID)); err != nil {
			return fmt.Errorf("failed to remove abandoned piece %d: %w", parkedPieceID, err)
		}
	}
	return nil
}

func (p *PDPService) cleanupExpiredDirectUploadClaims(ctx context.Context) error {
	var claims []struct {
		UploadID      string `db:"upload_id"`
		PieceRefID    int64  `db:"piece_ref_id"`
		ParkedPieceID int64  `db:"parked_piece_id"`
	}
	err := p.db.Select(ctx, &claims, `
		SELECT pu.id::TEXT AS upload_id,
		       pu.piece_ref AS piece_ref_id,
		       pp.id AS parked_piece_id
		FROM pdp_piece_uploads pu
		JOIN parked_piece_refs ppr ON ppr.ref_id = pu.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pu.piece_ref IS NOT NULL
		  AND pu.created_at <= NOW() - INTERVAL '1 hour'
		  AND ppr.data_url IS NULL
		  AND pp.complete = FALSE
		  AND pp.skip = TRUE
		ORDER BY pu.created_at, pu.id
		LIMIT 256
	`)
	if err != nil {
		return fmt.Errorf("select expired direct upload claims: %w", err)
	}

	var cleanupErr error
	for _, claim := range claims {
		if err := p.releaseDirectUploadClaim(ctx, claim.UploadID, claim.PieceRefID, claim.ParkedPieceID); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("release expired upload %s: %w", claim.UploadID, err))
		}
	}
	return cleanupErr
}

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
	if pieceInfo.RawSize < uint64(PieceSizeMinLimit) {
		httpServerError(w, http.StatusBadRequest, ErrPieceTooSmall.Error(), nil)
		return
	}
	if pieceInfo.RawSize > uint64(PieceSizeMaxLimit) {
		httpServerError(w, http.StatusRequestEntityTooLarge, ErrPieceTooLarge.Error(), nil)
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

			if err := insertPDPReference(tx, serviceID, pieceCidV1.String(), parkedPieceRefID, int64(size)); err != nil {
				return false, err
			}
			log.Debugw("[handlePiecePost] -- new pdp_piecerefs", "parkedPieceRefID", parkedPieceRefID, "pieceCidV1", pieceCidV1)

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

	// Lookup the expected piece and current claim from the database.
	var serviceID string
	var pieceCIDStr string
	var checkSize int64
	var pieceRef sql.NullInt64
	err = p.db.QueryRow(ctx, `
		SELECT service, piece_cid, piece_ref, check_size
		FROM pdp_piece_uploads
		WHERE id = $1
	`, uploadUUID.String()).Scan(&serviceID, &pieceCIDStr, &pieceRef, &checkSize)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpServerError(w, http.StatusNotFound, "Upload UUID not found", err)
		} else {
			httpServerError(w, http.StatusInternalServerError, "Database error", err)
		}
		return
	}
	log.Debugw("[handlePieceUpload] -- upload lookup done", "uploadUUID", uploadUUID)
	// A non-null ref is an active direct-write claim (or a completed legacy upload).
	if pieceRef.Valid {
		httpServerError(w, http.StatusConflict, "Data has already been uploaded", err)
		return
	}

	pieceCidV1, err := cid.Parse(pieceCIDStr)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to convert piece CID (v1): "+err.Error(), err)
		return
	}
	paddedSize := PadPieceSize(checkSize)
	claim, err := p.claimDirectUpload(ctx, uploadUUID.String(), serviceID, pieceCidV1.String(), checkSize, paddedSize)
	if err != nil {
		switch {
		case errors.Is(err, errUploadInProgress):
			httpServerError(w, http.StatusConflict, "This piece is already being uploaded", err)
		case errors.Is(err, errUploadClaimed):
			httpServerError(w, http.StatusConflict, "Data has already been uploaded", err)
		default:
			httpServerError(w, http.StatusInternalServerError, "Failed to claim piece upload", err)
		}
		return
	}
	if claim.complete {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cleanupClaim := true
	defer func() {
		if !cleanupClaim {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := p.releaseDirectUploadClaim(cleanupCtx, uploadUUID.String(), claim.pieceRefID, claim.parkedPieceID); err != nil {
			log.Errorw("failed to release direct upload claim", "uploadUUID", uploadUUID, "piece", claim.parkedPieceID, "error", err)
		}
	}()

	bodyReader := NewTimeoutLimitReader(r.Body, 5*time.Second)
	exactReader := &exactSizeReader{r: bodyReader, remaining: checkSize}
	pieceInfo, readSize, err := p.pieceIO.WriteUploadPiece(
		ctx,
		storiface.PieceNumber(claim.parkedPieceID),
		checkSize,
		exactReader,
		storiface.PathStorage,
		true,
	)
	if err != nil {
		if errors.Is(err, errPieceTooShort) {
			httpServerError(w, http.StatusBadRequest, "Piece size does not match the expected size", err)
		} else {
			log.Errorw("failed to write uploaded piece directly to storage", "uploadUUID", uploadUUID, "error", err)
			httpServerError(w, http.StatusInternalServerError, "Failed to store piece data", err)
		}
		return
	}
	if readSize != uint64(checkSize) {
		httpServerError(w, http.StatusBadRequest, "Piece size does not match the expected size", nil)
		return
	}

	hasExtra, err := readHasExtraByte(bodyReader)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to verify uploaded piece size", err)
		return
	}
	if hasExtra {
		msg := fmt.Sprintf("piece data exceeds the size declared in pieceCid (%d bytes)", checkSize)
		httpServerError(w, http.StatusBadRequest, msg, ErrExceedsDeclaredSize)
		return
	}
	if !pieceInfo.PieceCID.Equals(pieceCidV1) {
		log.Warnw("computed piece CID does not match expected piece CID", "computed", pieceInfo.PieceCID, "expected", pieceCidV1, "uploadUUID", uploadUUID)
		httpServerError(w, http.StatusBadRequest, "Computed piece CID does not match expected piece CID", nil)
		return
	}
	if pieceInfo.Size != abi.PaddedPieceSize(paddedSize) {
		httpServerError(w, http.StatusBadRequest, "Computed padded piece size does not match expected piece size", nil)
		return
	}

	finalizeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.finalizeDirectUpload(finalizeCtx, uploadUUID.String(), serviceID, pieceCidV1.String(), claim, checkSize); err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to finalize piece upload", err)
		return
	}
	cleanupClaim = false

	log.Debugw("[handlePieceUpload] -- piece upload done, writing response", "uploadUUID", uploadUUID)
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

		// already limited the maximum read size in TimeoutLimitReader
		if n < int64(PieceSizeMinLimit) {
			return ErrPieceTooSmall
		}

		readSize = n

		return nil
	}

	// Upload into StashStore
	stashID, err := p.storage.StashCreate(ctx, int64(PieceSizeMaxLimit), writeFunc)
	if err != nil {
		if errors.Is(err, ErrPieceTooLarge) {
			httpServerError(w, http.StatusRequestEntityTooLarge, ErrPieceTooLarge.Error(), err)
			return
		} else if errors.Is(err, ErrPieceTooSmall) {
			httpServerError(w, http.StatusBadRequest, ErrPieceTooSmall.Error(), err)
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
		parkedPieceID, err := parkpiece.Upsert(tx, pcid.String(), int64(paddedPieceSize), readSize, true)
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

func (t *TimeoutLimitReader) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.timeout)
	for {
		// Attempt to read
		n, err := t.r.Read(p)
		if t.totalBytes+int64(n) > int64(PieceSizeMaxLimit) {
			return 0, ErrPieceTooLarge
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
