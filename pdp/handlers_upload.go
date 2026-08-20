package pdp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multicodec"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"
	"github.com/yugabyte/pgx/v5/pgconn"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/parkpiece"
	"github.com/filecoin-project/curio/lib/piecestore"
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

type parkedPieceClaim struct {
	parkedPieceID int64
	pieceRefID    int64
	created       bool
	complete      bool
}

// claimParkedPiece returns a per-upload ref to the active long-term parked
// piece. A newly inserted skip=true row is owned by the caller and may be
// written directly. Existing incomplete rows remain owned by their current
// writer; existing complete rows can be reused without writing any bytes.
func claimParkedPiece(tx *harmonydb.Tx, pieceCID string, rawSize, paddedSize int64) (parkedPieceClaim, error) {
	var claim parkedPieceClaim
	var err error
	claim.parkedPieceID, claim.created, err = parkpiece.UpsertSkipWithInserted(tx, pieceCID, paddedSize, rawSize, true, true)
	if err != nil {
		return parkedPieceClaim{}, fmt.Errorf("failed to claim parked piece: %w", err)
	}

	if !claim.created {
		err = tx.QueryRow(`SELECT complete FROM parked_pieces WHERE id = $1`, claim.parkedPieceID).Scan(&claim.complete)
		if err != nil {
			return parkedPieceClaim{}, fmt.Errorf("failed to inspect existing parked piece: %w", err)
		}
		if !claim.complete {
			return parkedPieceClaim{}, errUploadInProgress
		}
	}

	err = tx.QueryRow(`
		INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, TRUE)
		RETURNING ref_id
	`, claim.parkedPieceID).Scan(&claim.pieceRefID)
	if err != nil {
		return parkedPieceClaim{}, fmt.Errorf("failed to create parked piece ref: %w", err)
	}

	return claim, nil
}

// claimDirectUpload atomically claims an unclaimed upload intent. For a new
// piece, it binds the intent to a skip=true parked-piece ref so this handler
// owns the direct write. If the piece became complete after POST, it publishes
// the PDP ref and consumes the intent without reading the request body.
func (p *PDPService) claimDirectUpload(ctx context.Context, uploadID, service, pieceCID string, rawSize, paddedSize int64) (parkedPieceClaim, error) {
	var claim parkedPieceClaim
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		claim, err = claimParkedPiece(tx, pieceCID, rawSize, paddedSize)
		if err != nil {
			return false, err
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
		return parkedPieceClaim{}, err
	}
	if !committed {
		return parkedPieceClaim{}, errors.New("failed to commit direct upload claim")
	}
	return claim, nil
}

// claimStreamingUpload binds the session to an upload-owned provisional piece
// before any request bytes are read. The provisional identity is replaced by
// the computed PieceCID after the one-pass final-storage write.
func (p *PDPService) claimStreamingUpload(ctx context.Context, uploadID, service string) (parkedPieceClaim, error) {
	calc := &commp.Calc{}
	defer calc.Reset()
	temporaryRawSize, err := io.WriteString(calc, uploadID+":"+uuid.NewString())
	if err != nil {
		return parkedPieceClaim{}, fmt.Errorf("failed to generate provisional piece identity: %w", err)
	}
	temporaryDigest, temporaryPaddedSize, err := calc.Digest()
	if err != nil {
		return parkedPieceClaim{}, fmt.Errorf("failed to generate provisional piece identity: %w", err)
	}
	temporaryCID, err := commcid.DataCommitmentV1ToCID(temporaryDigest)
	if err != nil {
		return parkedPieceClaim{}, fmt.Errorf("failed to generate provisional piece CID: %w", err)
	}

	var claim parkedPieceClaim
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		claim, err = claimParkedPiece(tx, temporaryCID.String(), int64(temporaryRawSize), int64(temporaryPaddedSize))
		if err != nil {
			return false, err
		}
		if !claim.created {
			return false, errUploadInProgress
		}

		n, err := tx.Exec(`
			UPDATE pdp_piece_streaming_uploads
			SET piece_ref = $1,
				created_at = NOW()
			WHERE id = $2
			  AND service = $3
			  AND piece_ref IS NULL
			  AND COALESCE(complete, FALSE) = FALSE
		`, claim.pieceRefID, uploadID, service)
		if err != nil {
			return false, fmt.Errorf("failed to claim streaming upload UUID: %w", err)
		}
		if n != 1 {
			return false, errUploadClaimed
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return parkedPieceClaim{}, err
	}
	if !committed {
		return parkedPieceClaim{}, errors.New("failed to commit streaming upload claim")
	}
	return claim, nil
}

func (p *PDPService) completeStreamingUpload(ctx context.Context, uploadID, service string, claim parkedPieceClaim, pieceInfo abi.PieceInfo, rawSize int64) error {
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var existingPieceID int64
		var existingRawSize int64
		var existingComplete bool
		err := tx.QueryRow(`
			SELECT id, piece_raw_size, complete
			FROM parked_pieces
			WHERE piece_cid = $1
			  AND piece_padded_size = $2
			  AND long_term = TRUE
			  AND cleanup_task_id IS NULL
			  AND id != $3
			ORDER BY id
			LIMIT 1
		`, pieceInfo.PieceCID.String(), int64(pieceInfo.Size), claim.parkedPieceID).Scan(&existingPieceID, &existingRawSize, &existingComplete)
		switch {
		case err == nil && (!existingComplete || existingRawSize != rawSize):
			return false, errUploadInProgress
		case err == nil:
			n, err := tx.Exec(`
				UPDATE parked_piece_refs
				SET piece_id = $1
				WHERE ref_id = $2 AND piece_id = $3
			`, existingPieceID, claim.pieceRefID, claim.parkedPieceID)
			if err != nil {
				return false, fmt.Errorf("failed to reuse completed parked piece: %w", err)
			}
			if n != 1 {
				return false, fmt.Errorf("failed to reuse completed parked piece: expected 1 row, got %d", n)
			}
		case errors.Is(err, pgx.ErrNoRows):
			n, err := tx.Exec(`
				UPDATE parked_pieces
				SET piece_cid = $1,
					piece_padded_size = $2,
					piece_raw_size = $3,
					complete = TRUE
				WHERE id = $4
				  AND complete = FALSE
				  AND skip = TRUE
				  AND cleanup_task_id IS NULL
			`, pieceInfo.PieceCID.String(), int64(pieceInfo.Size), rawSize, claim.parkedPieceID)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					return false, errUploadInProgress
				}
				return false, fmt.Errorf("failed to promote provisional parked piece: %w", err)
			}
			if n != 1 {
				return false, fmt.Errorf("failed to promote provisional parked piece: expected 1 row, got %d", n)
			}
		default:
			return false, fmt.Errorf("failed to inspect completed parked piece: %w", err)
		}

		n, err := tx.Exec(`
			UPDATE pdp_piece_streaming_uploads
			SET piece_cid = $1,
				piece_size = $2,
				raw_size = $3,
				complete = TRUE,
				completed_at = NOW()
			WHERE id = $4
			  AND service = $5
			  AND piece_ref = $6
			  AND COALESCE(complete, FALSE) = FALSE
		`, pieceInfo.PieceCID.String(), int64(pieceInfo.Size), rawSize, uploadID, service, claim.pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to mark streaming upload complete: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("failed to mark streaming upload complete: expected 1 row, got %d", n)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("failed to commit streaming upload completion")
	}
	return nil
}

func (p *PDPService) finalizeDirectUpload(ctx context.Context, uploadID, service, pieceCID string, claim parkedPieceClaim, rawSize int64) error {
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

// releaseStreamingUploadClaim resets a failed streaming session and drops its
// ref. The zero-ref provisional parked piece is left for normal piece cleanup.
func (p *PDPService) releaseStreamingUploadClaim(ctx context.Context, uploadID, service string, claim parkedPieceClaim) error {
	committed, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_piece_streaming_uploads
			SET piece_ref = NULL,
				piece_cid = NULL,
				piece_size = NULL,
				raw_size = NULL,
				complete = NULL,
				completed_at = NULL
			WHERE id = $1
			  AND service = $2
			  AND piece_ref = $3
			  AND COALESCE(complete, FALSE) = FALSE
		`, uploadID, service, claim.pieceRefID)
		if err != nil {
			return false, fmt.Errorf("failed to release streaming upload claim: %w", err)
		}
		if n == 0 {
			var retained bool
			err = tx.QueryRow(`
				SELECT EXISTS(
					SELECT 1
					FROM pdp_piece_streaming_uploads
					WHERE id = $1 AND service = $2 AND piece_ref = $3 AND complete = TRUE
					UNION ALL
					SELECT 1 FROM pdp_piecerefs WHERE piece_ref = $3
				)
			`, uploadID, service, claim.pieceRefID).Scan(&retained)
			if err != nil {
				return false, fmt.Errorf("failed to check streaming upload completion: %w", err)
			}
			if retained {
				return true, nil
			}
		}
		if n > 1 {
			return false, fmt.Errorf("failed to release streaming upload claim: expected 1 row, got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1 AND piece_id = $2`, claim.pieceRefID, claim.parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to delete streaming upload ref: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("failed to commit streaming upload claim release")
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

func (p *PDPService) cleanupExpiredStreamingUploadClaims(ctx context.Context) error {
	var claims []struct {
		UploadID      string `db:"upload_id"`
		Service       string `db:"service"`
		PieceRefID    int64  `db:"piece_ref_id"`
		ParkedPieceID int64  `db:"parked_piece_id"`
	}
	err := p.db.Select(ctx, &claims, `
		SELECT su.id::TEXT AS upload_id,
		       su.service,
		       su.piece_ref AS piece_ref_id,
		       pp.id AS parked_piece_id
		FROM pdp_piece_streaming_uploads su
		JOIN parked_piece_refs ppr ON ppr.ref_id = su.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE su.piece_ref IS NOT NULL
		  AND COALESCE(su.complete, FALSE) = FALSE
		  AND su.created_at <= NOW() - INTERVAL '1 hour'
		  AND ppr.data_url IS NULL
		  AND pp.complete = FALSE
		  AND pp.skip = TRUE
		ORDER BY su.created_at, su.id
		LIMIT 256
	`)
	if err != nil {
		return fmt.Errorf("select expired streaming upload claims: %w", err)
	}

	var cleanupErr error
	for _, stale := range claims {
		claim := parkedPieceClaim{
			parkedPieceID: stale.ParkedPieceID,
			pieceRefID:    stale.PieceRefID,
			created:       true,
		}
		if err := p.releaseStreamingUploadClaim(ctx, stale.UploadID, stale.Service, claim); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("release expired streaming upload %s: %w", stale.UploadID, err))
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

	bodyReader := NewTimeoutLimitReader(r.Body, 5*time.Second)
	prefix := make([]byte, int(PieceSizeMinLimit))
	if _, err := io.ReadFull(bodyReader, prefix); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			httpServerError(w, http.StatusBadRequest, ErrPieceTooSmall.Error(), ErrPieceTooSmall)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to read piece data", err)
		return
	}

	claim, err := p.claimStreamingUpload(ctx, uploadUUID.String(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, errUploadInProgress):
			httpServerError(w, http.StatusConflict, "This piece is already being uploaded", err)
		case errors.Is(err, errUploadClaimed):
			httpServerError(w, http.StatusConflict, "Data has already been uploaded", err)
		default:
			log.Errorw("Failed to claim streaming upload", "uploadUUID", uploadUUID, "error", err)
			httpServerError(w, http.StatusInternalServerError, "Failed to claim streaming upload", err)
		}
		return
	}

	cleanupClaim := true
	defer func() {
		if !cleanupClaim {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := p.releaseStreamingUploadClaim(cleanupCtx, uploadUUID.String(), serviceID, claim); err != nil {
			log.Errorw("failed to release streaming upload claim", "uploadUUID", uploadUUID, "piece", claim.parkedPieceID, "error", err)
		}
	}()

	pieceInfo, readSize, err := p.pieceIO.WriteUploadPiece(
		ctx,
		storiface.PieceNumber(claim.parkedPieceID),
		int64(PieceSizeMaxLimit),
		io.MultiReader(bytes.NewReader(prefix), bodyReader),
		storiface.PathStorage,
		false,
	)
	if err != nil {
		if errors.Is(err, ErrPieceTooLarge) || errors.Is(err, piecestore.ErrPieceTooLarge) {
			httpServerError(w, http.StatusRequestEntityTooLarge, ErrPieceTooLarge.Error(), err)
			return
		}
		log.Errorw("Failed to write streaming upload directly to storage", "uploadUUID", uploadUUID, "error", err)
		httpServerError(w, http.StatusInternalServerError, "Failed to store piece data", err)
		return
	}

	completeCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.completeStreamingUpload(completeCtx, uploadUUID.String(), serviceID, claim, pieceInfo, int64(readSize)); err != nil {
		if errors.Is(err, errUploadInProgress) {
			httpServerError(w, http.StatusConflict, "This piece is already being uploaded or conflicts with an existing piece", err)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to complete streaming upload", err)
		return
	}
	cleanupClaim = false

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

	// Query database for stored piece info
	var dPcidStr string
	var pref int64
	var rawSize uint64

	err = p.db.QueryRow(ctx, `
		SELECT su.piece_cid, su.piece_ref, su.raw_size
		FROM pdp_piece_streaming_uploads su
		JOIN parked_piece_refs ppr ON ppr.ref_id = su.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE su.id = $1
		  AND su.service = $2
		  AND su.complete = TRUE
		  AND pp.complete = TRUE
	`, uploadUUID.String(), serviceID).Scan(&dPcidStr, &pref, &rawSize)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpServerError(w, http.StatusConflict, "Streaming upload is not complete", err)
			return
		}
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
			INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
			SELECT su.service, su.piece_cid, su.piece_ref, NOW(), $4
			FROM pdp_piece_streaming_uploads su
			JOIN parked_piece_refs ppr ON ppr.ref_id = su.piece_ref
			JOIN parked_pieces pp ON pp.id = ppr.piece_id
			WHERE su.id = $1
			  AND su.service = $2
			  AND su.piece_ref = $3
			  AND su.complete = TRUE
			  AND pp.complete = TRUE
		`, uploadUUID.String(), serviceID, pref, needsSaveCache(int64(rawSize)))
		if err != nil {
			return false, fmt.Errorf("failed to create PDP piece reference: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("failed to create PDP piece reference: expected 1 row but got %d", n)
		}

		n, err = tx.Exec(`
			DELETE FROM pdp_piece_streaming_uploads
			WHERE id = $1 AND service = $2 AND piece_ref = $3 AND complete = TRUE
		`, uploadUUID.String(), serviceID, pref)
		if err != nil {
			return false, fmt.Errorf("failed to delete pdp_piece_streaming_uploads entry: %w", err)
		}
		if n != 1 {
			return false, fmt.Errorf("failed to delete pdp_piece_streaming_uploads entry: expected 1 row but got %d", n)
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
