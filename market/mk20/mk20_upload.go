package mk20

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"
)

// HandleUploadStatus retrieves and returns the upload status of a deal, including chunk completion details, or reports errors if the process fails.
// @param ID ulid.ULID
// @Return UploadStatusCode

func (m *MK20) HandleUploadStatus(ctx context.Context, id ulid.ULID, w http.ResponseWriter) {
	var exists bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS (
									  SELECT 1
									  FROM market_mk20_upload_waiting
									  WHERE id = $1
										AND (chunked IS NULL OR chunked = TRUE)
									);`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if upload is waiting for data", "deal", id, "error", err)
		w.WriteHeader(int(UploadStatusCodeServerError))
		return
	}
	if !exists {
		http.Error(w, "deal not found", int(UploadStatusCodeDealNotFound))
		return
	}

	var ret UploadStatus

	err = m.DB.QueryRow(ctx, `SELECT
								  COUNT(*) AS total,
								  COUNT(*) FILTER (WHERE complete) AS complete,
								  COUNT(*) FILTER (WHERE NOT complete) AS missing,
								  ARRAY_AGG(chunk ORDER BY chunk) FILTER (WHERE complete) AS completed_chunks,
								  ARRAY_AGG(chunk ORDER BY chunk) FILTER (WHERE NOT complete) AS incomplete_chunks
								FROM
								  market_mk20_deal_chunk
								WHERE
								  id = $1
								GROUP BY
								  id;`, id.String()).Scan(&ret.TotalChunks, &ret.Uploaded, &ret.Missing, &ret.UploadedChunks, &ret.MissingChunks)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Errorw("failed to get upload status", "deal", id, "error", err)
			w.WriteHeader(int(UploadStatusCodeServerError))
			return
		}

		http.Error(w, "upload not initiated", int(UploadStatusCodeUploadNotStarted))
		return
	}

	data, err := json.Marshal(ret)
	if err != nil {
		log.Errorw("failed to marshal upload status", "deal", id, "error", err)
		w.WriteHeader(int(UploadStatusCodeServerError))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(int(UploadStatusCodeOk))

	_, err = w.Write(data)
	if err != nil {
		log.Errorw("failed to write upload status", "deal", id, "error", err)
	}
}

// HandleUploadStart handles the initialization of a file upload process for a specific deal, validating input and creating database entries.
// @param ID ulid.ULID
// @param upload StartUpload
// @Return UploadStartCode

func (m *MK20) HandleUploadStart(ctx context.Context, id ulid.ULID, upload StartUpload, w http.ResponseWriter) {
	chunkSize := upload.ChunkSize
	if upload.RawSize == 0 {
		log.Errorw("raw size must be greater than 0", "id", id)
		http.Error(w, "raw size must be greater than 0", int(UploadStartCodeBadRequest))
		return
	}

	if chunkSize == 0 {
		log.Errorw("chunk size must be greater than 0", "id", id)
		http.Error(w, "chunk size must be greater than 0", int(UploadStartCodeBadRequest))
		return
	}

	// Check if chunk size is a power of 2
	if chunkSize&(chunkSize-1) != 0 {
		log.Errorw("chunk size must be a power of 2", "id", id)
		http.Error(w, "chunk size must be a power of 2", int(UploadStartCodeBadRequest))
		return
	}

	// Check that chunk size align with config
	if chunkSize < m.cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize {
		log.Errorw("chunk size too small", "id", id)
		http.Error(w, "chunk size too small", int(UploadStartCodeBadRequest))
		return
	}
	if chunkSize > m.cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize {
		log.Errorw("chunk size too large", "id", id)
		http.Error(w, "chunk size too large", int(UploadStartCodeBadRequest))
		return
	}

	// Check if deal exists
	var exists bool

	err := m.DB.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_upload_waiting
								  WHERE id = $1 AND chunked IS NULL);`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if deal is waiting for upload to start", "deal", id, "error", err)
		http.Error(w, "", int(UploadStartCodeServerError))
		return
	}
	if !exists {
		http.Error(w, "deal not found", int(UploadStartCodeDealNotFound))
		return
	}

	// Check if we already started the upload
	var started bool
	err = m.DB.QueryRow(ctx, `SELECT EXISTS (
									SELECT 1 
									FROM market_mk20_deal_chunk 
									WHERE id = $1);`, id.String()).Scan(&started)
	if err != nil {
		log.Errorw("failed to check if deal upload has started", "deal", id, "error", err)
		http.Error(w, "", int(UploadStartCodeServerError))
		return
	}

	if started {
		http.Error(w, "deal upload has already started", int(UploadStartCodeAlreadyStarted))
		return
	}

	deal, err := DealFromDB(ctx, m.DB, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		http.Error(w, "", int(UploadStartCodeServerError))
		return
	}

	var rawSize uint64

	if deal.Data != nil {
		rawSize, err = deal.RawSize()
		if err != nil {
			log.Errorw("failed to get raw size of deal", "deal", id, "error", err)
			http.Error(w, "", int(UploadStartCodeServerError))
			return
		}
		if rawSize != upload.RawSize {
			log.Errorw("raw size of deal does not match the one provided in deal", "deal", id, "error", err)
			http.Error(w, "", int(UploadStartCodeBadRequest))
		}
	}

	numChunks := int(math.Ceil(float64(rawSize) / float64(chunkSize)))

	// Create rows in market_mk20_deal_chunk for each chunk for the ID
	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		batch := &pgx.Batch{}
		batchSize := 15000
		for i := 1; i <= numChunks; i++ {
			if i < numChunks {
				batch.Queue(`INSERT INTO market_mk20_deal_chunk (id, chunk, chunk_size, complete) VALUES ($1, $2, $3, FALSE);`, id.String(), i, chunkSize)
			} else {
				// Calculate the size of last chunk
				s := int64(rawSize) - (int64(numChunks-1) * chunkSize)
				if s <= 0 || s > chunkSize {
					return false, xerrors.Errorf("invalid chunk size")
				}

				batch.Queue(`INSERT INTO market_mk20_deal_chunk (id, chunk, chunk_size, complete) VALUES ($1, $2, $3, FALSE);`, id.String(), i, s)
			}
			if batch.Len() >= batchSize {
				res := tx.SendBatch(ctx, batch)
				if err := res.Close(); err != nil {
					return false, xerrors.Errorf("closing insert chunk batch: %w", err)
				}
				batch = &pgx.Batch{}
			}
		}
		if batch.Len() > 0 {
			res := tx.SendBatch(ctx, batch)
			if err := res.Close(); err != nil {
				return false, xerrors.Errorf("closing insert chunk batch: %w", err)
			}
		}
		n, err := tx.Exec(`UPDATE market_mk20_upload_waiting SET chunked = TRUE WHERE id = $1`, id.String())
		if err != nil {
			return false, xerrors.Errorf("updating chunked flag: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("updating chunked flag: expected 1 row updated, got %d", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("failed to create chunks for deal", "deal", id, "error", err)
		http.Error(w, "", int(UploadStartCodeServerError))
		return
	}
	if !comm {
		log.Errorw("failed to create chunks for deal", "deal", id, "error", err)
		http.Error(w, "", int(UploadStartCodeServerError))
		return
	}
	w.WriteHeader(int(UploadStartCodeOk))
}

// HandleUploadChunk processes a single chunk upload for a deal and validates its state.
// It checks if the chunk exists, ensures it's not already uploaded, and stores it if valid.
// The function updates the database with chunk details and manages transaction rolls back on failure.
// @param id ulid.ULID
// @param chunk int
// @param data []byte
// @Return UploadCode

func (m *MK20) HandleUploadChunk(id ulid.ULID, chunk int, data io.ReadCloser, w http.ResponseWriter) {
	if m.maxParallelUploads.Load()+1 > int64(m.cfg.Market.StorageMarketConfig.MK20.MaxParallelChunkUploads) {
		log.Errorw("max parallel uploads reached", "deal", id, "chunk", chunk, "error", "max parallel uploads reached")
		http.Error(w, "too many parallel uploads for provider", int(UploadRateLimit))
		return
	}

	ctx := context.Background()
	defer func() {
		_ = data.Close()
	}()

	if chunk < 1 {
		http.Error(w, "chunk must be greater than 0", int(UploadBadRequest))
		return
	}

	var chunkDetails []struct {
		Chunk    int           `db:"chunk"`
		Size     int64         `db:"chunk_size"`
		Complete bool          `db:"complete"`
		RefID    sql.NullInt64 `db:"ref_id"`
	}
	err := m.DB.Select(ctx, &chunkDetails, `SELECT chunk, chunk_size, ref_id, complete 
								  FROM market_mk20_deal_chunk
								  WHERE id = $1 AND chunk = $2`, id.String(), chunk)
	if err != nil {
		log.Errorw("failed to check if chunk exists", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
	}

	if len(chunkDetails) == 0 {
		http.Error(w, "chunk not found", int(UploadNotFound))
		return
	}

	if len(chunkDetails) > 1 {
		log.Errorw("chunk exists multiple times", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if chunkDetails[0].Complete {
		http.Error(w, "chunk already uploaded", int(UploadChunkAlreadyUploaded))
		return
	}

	if chunkDetails[0].RefID.Valid {
		http.Error(w, "chunk already uploaded", int(UploadChunkAlreadyUploaded))
		return
	}

	log.Debugw("uploading chunk", "deal", id, "chunk", chunk)

	chunkSize := chunkDetails[0].Size
	reader := NewTimeoutLimitReader(data, time.Second*5)
	m.maxParallelUploads.Add(1)
	defer func() {
		m.maxParallelUploads.Add(-1)
	}()

	// Generate unique tmp pieceCID and Size for parked_pieces tables
	wr := new(commp.Calc)
	defer wr.Reset()
	n, err := fmt.Fprintf(wr, "%s, %d, %d, %s", id.String(), chunk, chunkSize, time.Now().String())
	if err != nil {
		log.Errorw("failed to generate unique tmp pieceCID and Size for parked_pieces tables", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}
	digest, tsize, err := wr.Digest()
	if err != nil {
		panic(err)
	}

	tpcid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		log.Errorw("failed to generate unique tmp pieceCID and Size for parked_pieces tables", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "failed to generate piece cid"+err.Error(), int(UploadServerError))
		return
	}
	var pnum, refID int64

	// Generate piece park details with tmp pieceCID and Size
	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.QueryRow(`SELECT id FROM parked_pieces 
          					WHERE piece_cid = $1 
          					  AND piece_padded_size = $2 
          					  AND piece_raw_size = $3`, tpcid.String(), tsize, n).Scan(&pnum)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
							VALUES ($1, $2, $3, FALSE, TRUE)
							ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
							RETURNING id`, tpcid.String(), tsize, n).Scan(&pnum)
				if err != nil {
					return false, xerrors.Errorf("inserting new parked piece and getting id: %w", err)
				}
			} else {
				return false, xerrors.Errorf("checking existing parked piece: %w", err)
			}
		}

		// Add parked_piece_ref
		err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
        			VALUES ($1, $2, FALSE) RETURNING ref_id`, pnum, "/PUT").Scan(&refID)
		if err != nil {
			return false, xerrors.Errorf("inserting parked piece ref: %w", err)
		}

		return true, nil
	})

	if err != nil {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", "failed to commit transaction")
		http.Error(w, "", int(UploadServerError))
		return
	}

	log.Debugw("tmp piece details generated for the chunk", "deal", id, "chunk", chunk)

	failed := true
	defer func() {
		if failed {
			_, err = m.DB.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
			if err != nil {
				log.Errorw("failed to delete parked piece ref", "deal", id, "chunk", chunk, "error", err)
			}
		}
	}()

	// Store the piece and generate PieceCID and Size
	pi, _, err := m.sc.WriteUploadPiece(ctx, storiface.PieceNumber(pnum), chunkSize, reader, storiface.PathSealing, true)
	if err != nil {
		log.Errorw("failed to write piece", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	log.Debugw("piece stored", "deal", id, "chunk", chunk)

	// Update piece park details with correct values
	comm, err = m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE parked_pieces SET 
                         piece_cid = $1, 
                         piece_padded_size = $2, 
                         piece_raw_size = $3, 
                         complete = true
                     WHERE id = $4`,
			pi.PieceCID.String(), pi.Size, chunkSize, pnum)
		if err != nil {
			return false, xerrors.Errorf("updating parked piece: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("updating parked piece: expected 1 row updated, got %d", n)
		}

		n, err = tx.Exec(`UPDATE market_mk20_deal_chunk SET 
                                  complete = TRUE,
                                  completed_at = NOW() AT TIME ZONE 'UTC',
                                  ref_id = $1 
                              WHERE id = $2 
                                AND chunk = $3
                                AND complete = FALSE
                                AND ref_id IS NULL`, refID, id.String(), chunk)
		if err != nil {
			return false, xerrors.Errorf("updating chunk url: %w", err)
		}

		return n == 1, nil
	})

	if err != nil {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", "failed to commit transaction")
		http.Error(w, "", int(UploadServerError))
		return
	}

	log.Debugw("chunk upload finished", "deal", id, "chunk", chunk)

	failed = false
	w.WriteHeader(int(UploadOk))
}

// HandleUploadFinalize completes the upload process for a deal by verifying its chunks, updating the deal, and marking the upload as finalized.
// @param id ulid.ULID
// @param deal *Deal [optional]
// @Return DealCode

func (m *MK20) HandleUploadFinalize(id ulid.ULID, deal *Deal, w http.ResponseWriter, auth string) {
	ctx := context.Background()
	var exists bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_deal_chunk
								  WHERE id = $1 AND complete = FALSE OR complete IS NULL
								)`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if deal upload has started", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if exists {
		http.Error(w, "deal upload has not finished", http.StatusBadRequest)
		return
	}

	ddeal, err := DealFromDB(ctx, m.DB, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if ddeal.Data == nil && deal == nil {
		log.Errorw("cannot finalize deal with missing data source", "deal", id)
		http.Error(w, "cannot finalize deal with missing data source", int(ErrBadProposal))
		return
	}

	var rawSize uint64
	var newDeal *Deal
	var dealUpdated bool

	if deal != nil {
		// This is a deal where DataSource was not set - we should update the deal
		code, ndeal, _, err := m.updateDealDetails(id, deal, auth)
		if err != nil {
			log.Errorw("failed to update deal details", "deal", id, "error", err)
			if code == ErrServerInternalError {
				http.Error(w, "", int(ErrServerInternalError))
			} else {
				http.Error(w, err.Error(), int(code))
			}
			return
		}
		rawSize, err = ndeal.RawSize()
		if err != nil {
			log.Errorw("failed to get raw size of deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		newDeal = ndeal
		dealUpdated = true
	} else {
		rawSize, err = ddeal.RawSize()
		if err != nil {
			log.Errorw("failed to get raw size of deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		newDeal = ddeal
	}

	var valid bool

	err = m.DB.QueryRow(ctx, `SELECT SUM(chunk_size) = $2 AS valid
								FROM market_mk20_deal_chunk
								WHERE id = $1;`, id.String(), rawSize).Scan(&valid)
	if err != nil {
		log.Errorw("failed to check if deal upload has started", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}
	if !valid {
		log.Errorw("deal upload finalize failed", "deal", id, "error", "deal raw size does not match the sum of chunks")
		http.Error(w, "deal raw size does not match the sum of chunks", int(ErrBadProposal))
		return
	}

	if newDeal.Products.DDOV1 != nil {
		rej, err := m.sanitizeDDODeal(ctx, newDeal)
		if err != nil {
			log.Errorw("failed to sanitize DDO deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		if rej != nil {
			if rej.HTTPCode == 500 {
				log.Errorw("failed to sanitize DDO deal", "deal", id, "error", rej.Reason)
				http.Error(w, "", int(ErrServerInternalError))
				return
			}
			if rej.HTTPCode != 500 {
				log.Errorw("failed to sanitize DDO deal", "deal", id, "error", rej.Reason)
				http.Error(w, rej.Reason, int(rej.HTTPCode))
				return
			}
		}
	}

	if newDeal.Products.PDPV1 != nil {
		rej, err := m.sanitizePDPDeal(ctx, newDeal)
		if err != nil {
			log.Errorw("failed to sanitize PDP deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		if rej != nil {
			if rej.HTTPCode == 500 {
				log.Errorw("failed to sanitize PDP deal", "deal", id, "error", rej.Reason)
				http.Error(w, "", int(ErrServerInternalError))
				return
			}
			if rej.HTTPCode != 500 {
				log.Errorw("failed to sanitize PDP deal", "deal", id, "error", rej.Reason)
				http.Error(w, rej.Reason, int(rej.HTTPCode))
				return
			}
		}
	}

	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Now update the upload status to trigger the correct pipeline
		n, err := tx.Exec(`UPDATE market_mk20_deal_chunk SET finalize = TRUE where id = $1`, id.String())
		if err != nil {
			log.Errorw("failed to finalize deal upload", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}

		if n == 0 {
			return false, xerrors.Errorf("expected to update %d rows, got 0", n)
		}

		_, err = tx.Exec(`DELETE FROM market_mk20_upload_waiting WHERE id = $1`, id.String())
		if err != nil {
			return false, xerrors.Errorf("failed to delete upload waiting: %w", err)
		}

		if dealUpdated {
			// Save the updated deal to DB
			err = newDeal.UpdateDeal(tx)
			if err != nil {
				return false, xerrors.Errorf("failed to update deal: %w", err)
			}
		}
		return true, nil
	})

	if err != nil {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if !comm {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", "failed to commit transaction")
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	w.WriteHeader(int(Ok))
}

func (m *MK20) updateDealDetails(id ulid.ULID, deal *Deal, auth string) (DealCode, *Deal, []ProductName, error) {
	ctx := context.Background() // Let's not use request context to avoid DB inconsistencies

	if deal.Identifier.Compare(id) != 0 {
		return ErrBadProposal, nil, nil, xerrors.Errorf("deal ID and proposal ID do not match")
	}

	if deal.Data == nil {
		return ErrBadProposal, nil, nil, xerrors.Errorf("deal data is nil")
	}

	// Validate the deal
	code, err := deal.Validate(m.DB, &m.cfg.Market.StorageMarketConfig.MK20, auth)
	if err != nil {
		return code, nil, nil, err
	}

	log.Debugw("deal validated", "deal", deal.Identifier.String())

	// Verify we have a deal is DB
	var exists bool
	err = m.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_deal WHERE id = $1)`, id.String()).Scan(&exists)
	if err != nil {
		return ErrServerInternalError, nil, nil, xerrors.Errorf("failed to check if deal exists: %w", err)
	}

	if !exists {
		return ErrDealNotFound, nil, nil, xerrors.Errorf("deal not found")
	}

	// Get updated deal
	ndeal, code, np, err := UpdateDealDetails(ctx, m.DB, id, deal, &m.cfg.Market.StorageMarketConfig.MK20, auth)
	if err != nil {
		return code, nil, nil, err
	}

	return Ok, ndeal, np, nil
}

func (m *MK20) HandleSerialUpload(id ulid.ULID, body io.Reader, w http.ResponseWriter) {
	if m.maxParallelUploads.Load()+1 > int64(m.cfg.Market.StorageMarketConfig.MK20.MaxParallelChunkUploads) {
		log.Errorw("max parallel uploads reached", "deal", id, "error", "max parallel uploads reached")
		http.Error(w, "too many parallel uploads for provider", int(UploadRateLimit))
		return
	}

	ctx := context.Background()
	var exists bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS (
									  SELECT 1
									  FROM market_mk20_upload_waiting
									  WHERE id = $1 AND chunked IS NULL);`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if upload is waiting for data", "deal", id, "error", err)
		w.WriteHeader(int(UploadServerError))
		return
	}
	if !exists {
		http.Error(w, "deal not found", int(UploadStartCodeDealNotFound))
		return
	}

	reader := NewTimeoutLimitReader(body, time.Second*5)
	m.maxParallelUploads.Add(1)
	defer func() {
		m.maxParallelUploads.Add(-1)
	}()

	// Generate unique tmp pieceCID and Size for parked_pieces tables
	wr := new(commp.Calc)
	defer wr.Reset()
	trs, err := fmt.Fprintf(wr, "%s, %s", id.String(), time.Now().String())
	if err != nil {
		log.Errorw("failed to generate unique tmp pieceCID and Size for parked_pieces tables", "deal", id, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}
	digest, tsize, err := wr.Digest()
	if err != nil {
		panic(err)
	}

	trSize := uint64(trs)

	tpcid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		log.Errorw("failed to generate tmp pieceCID", "deal", id, "error", err)
		http.Error(w, "failed to generate piece cid"+err.Error(), int(UploadServerError))
		return
	}

	deal, err := DealFromDB(ctx, m.DB, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	var havePInfo bool
	var pinfo *PieceInfo

	if deal.Data != nil {
		pi, err := deal.PieceInfo()
		if err != nil {
			log.Errorw("failed to get piece info from deal", "deal", id, "error", err)
			http.Error(w, "", int(UploadServerError))
		}

		tpcid = pi.PieceCIDV1
		tsize = uint64(pi.Size)
		trSize = pi.RawSize
		havePInfo = true
		pinfo = pi
	}

	var pnum, refID int64
	pieceExists := true

	// Generate piece park details with tmp pieceCID and Size
	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.QueryRow(`SELECT id FROM parked_pieces 
          					WHERE piece_cid = $1 
          					  AND piece_padded_size = $2 
          					  AND piece_raw_size = $3
          					  AND complete = true`, tpcid.String(), tsize, trSize).Scan(&pnum)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term, skip)
							VALUES ($1, $2, $3, TRUE, TRUE)
							ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
							RETURNING id`, tpcid.String(), tsize, trSize).Scan(&pnum)
				if err != nil {
					return false, xerrors.Errorf("inserting new parked piece and getting id: %w", err)
				}
				pieceExists = false
			} else {
				return false, xerrors.Errorf("checking existing parked piece: %w", err)
			}
		}

		// Add parked_piece_ref
		err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
        			VALUES ($1, $2, TRUE) RETURNING ref_id`, pnum, "/PUT").Scan(&refID)
		if err != nil {
			return false, xerrors.Errorf("inserting parked piece ref: %w", err)
		}

		// Mark upload as started to prevent someone else from using chunk upload
		n, err := tx.Exec(`UPDATE market_mk20_upload_waiting SET chunked = FALSE WHERE id = $1`, id.String())
		if err != nil {
			return false, xerrors.Errorf("updating upload waiting: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("updating upload waiting: expected 1 row updated, got %d", n)
		}

		return true, nil
	})

	if err != nil {
		log.Errorw("failed to update chunk", "deal", id, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "error", "failed to commit transaction")
		http.Error(w, "", int(UploadServerError))
		return
	}

	// If we know the piece details and already have it then let's return early
	if pieceExists && havePInfo {
		w.WriteHeader(int(UploadOk))
	}

	if !havePInfo {
		log.Debugw("tmp piece details generated for the chunk", "deal", id)
	}

	failed := true
	defer func() {
		if failed {
			_, serr := m.DB.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
			if serr != nil {
				log.Errorw("failed to delete parked piece ref", "deal", id, "error", serr)
			}

			_, serr = m.DB.Exec(ctx, `UPDATE market_mk20_upload_waiting SET chunked = NULL WHERE id = $1`, id.String())
			if serr != nil {
				log.Errorw("failed to update upload waiting", "deal", id, "error", serr)
			}
		}
	}()

	// Store the piece and generate PieceCID and Size
	pi, rawSize, err := m.sc.WriteUploadPiece(ctx, storiface.PieceNumber(pnum), UploadSizeLimit, reader, storiface.PathSealing, false)
	if err != nil {
		log.Errorw("failed to write piece", "deal", id, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if havePInfo {
		if rawSize != pinfo.RawSize {
			log.Errorw("piece raw size does not match", "deal", id, "supplied", pinfo.RawSize, "written", rawSize, "error", "piece raw size does not match")
			http.Error(w, "piece raw size does not match", int(UploadBadRequest))
			return
		}

		if !pi.PieceCID.Equals(pinfo.PieceCIDV1) {
			log.Errorw("piece CID does not match", "deal", id, "error", "piece CID does not match")
			http.Error(w, "piece CID does not match", int(UploadBadRequest))
			return
		}
		if pi.Size != pinfo.Size {
			log.Errorw("piece size does not match", "deal", id, "error", "piece size does not match")
			http.Error(w, "piece size does not match", int(UploadBadRequest))
			return
		}
	}

	log.Debugw("piece stored", "deal", id)

	// Update piece park details with correct values
	comm, err = m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		if havePInfo {
			n, err := tx.Exec(`UPDATE parked_pieces SET complete = true WHERE id = $1`, pnum)
			if err != nil {
				return false, xerrors.Errorf("updating parked piece: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("updating parked piece: expected 1 row updated, got %d", n)
			}
		} else {
			var pid int64
			var complete bool
			// Check if we already have the piece, if found then verify access and skip rest of the processing
			err = tx.QueryRow(`SELECT id, complete FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2 AND long_term = TRUE`, pi.PieceCID.String(), pi.Size).Scan(&pid, &complete)
			if err == nil {
				if complete {
					// If piece exists then check if we can access the data
					pr, err := m.sc.PieceReader(ctx, storiface.PieceNumber(pid))
					if err != nil {
						// We should fail here because any subsequent operation which requires access to data will also fail
						// till this error is fixed
						if !errors.Is(err, storiface.ErrSectorNotFound) {
							return false, fmt.Errorf("failed to get piece reader: %w", err)
						}

						// If piece does not exist then we update piece park table to work with new tmpID
						// Update ref table's reference to tmp id
						_, err = tx.Exec(`UPDATE parked_piece_refs SET piece_id = $1 WHERE piece_id = $2`, pnum, pid)
						if err != nil {
							return false, xerrors.Errorf("updating parked piece ref: %w", err)
						}

						// Now delete the original piece which has 404 error
						_, err = tx.Exec(`DELETE FROM parked_pieces WHERE id = $1`, pid)
						if err != nil {
							return false, xerrors.Errorf("deleting parked piece: %w", err)
						}

						// Update the tmp entry with correct details
						n, err := tx.Exec(`UPDATE parked_pieces SET 
													 piece_cid = $1, 
													 piece_padded_size = $2, 
													 piece_raw_size = $3, 
													 complete = true
												 WHERE id = $4`,
							pi.PieceCID.String(), pi.Size, rawSize, pnum)
						if err != nil {
							return false, xerrors.Errorf("updating parked piece: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("updating parked piece: expected 1 row updated, got %d", n)
						}
					} else {
						defer func() {
							_ = pr.Close()
						}()
						// Add parked_piece_ref if no errors
						var newRefID int64
						err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
        										VALUES ($1, $2, FALSE) RETURNING ref_id`, pid, "/PUT").Scan(&newRefID)
						if err != nil {
							return false, xerrors.Errorf("inserting parked piece ref: %w", err)
						}

						// Remove the tmp refs. This will also delete the new tmp parked_pieces entry
						_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
						if err != nil {
							return false, xerrors.Errorf("deleting tmp parked piece ref: %w", err)
						}
						// Update refID to be used later
						refID = newRefID
					}
				} else {
					n, err := tx.Exec(`UPDATE parked_pieces SET complete = true WHERE id = $1`, pid)
					if err != nil {
						return false, xerrors.Errorf("updating parked piece: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("updating parked piece: expected 1 row updated, got %d", n)
					}
				}
			} else {
				if !errors.Is(err, pgx.ErrNoRows) {
					return false, fmt.Errorf("failed to check if piece already exists: %w", err)
				}
				// If piece does not exist then let's update the tmp one
				n, err := tx.Exec(`UPDATE parked_pieces SET 
                         piece_cid = $1, 
                         piece_padded_size = $2, 
                         piece_raw_size = $3, 
                         complete = true
                     WHERE id = $4`,
					pi.PieceCID.String(), pi.Size, rawSize, pnum)
				if err != nil {
					return false, xerrors.Errorf("updating parked piece: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("updating parked piece: expected 1 row updated, got %d", n)
				}
			}
		}

		n, err := tx.Exec(`UPDATE market_mk20_upload_waiting SET chunked = FALSE, ref_id = $2 WHERE id = $1`, id.String(), refID)
		if err != nil {
			return false, xerrors.Errorf("updating upload waiting: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("updating upload waiting: expected 1 row updated, got %d", n)
		}
		return true, nil
	})

	if err != nil {
		log.Errorw("failed to update chunk", "deal", id, "error", err)
		http.Error(w, "", int(UploadServerError))
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "error", "failed to commit transaction")
		http.Error(w, "", int(UploadServerError))
		return
	}

	log.Debugw("chunk upload finished", "deal", id)

	failed = false
	w.WriteHeader(int(UploadOk))
}

func (m *MK20) HandleSerialUploadFinalize(id ulid.ULID, deal *Deal, w http.ResponseWriter, auth string) {
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1<<16)
			n := runtime.Stack(trace, false)
			log.Errorf("panic occurred: %v\n%s", r, trace[:n])
			debug.PrintStack()
		}
	}()

	ctx := context.Background()
	var exists bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS (
									  SELECT 1
									  FROM market_mk20_upload_waiting
									  WHERE id = $1 AND chunked = FALSE AND ref_id IS NOT NULL);`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if upload is waiting for data", "deal", id, "error", err)
		w.WriteHeader(int(ErrServerInternalError))
		return
	}

	if !exists {
		http.Error(w, "deal not found", int(ErrDealNotFound))
		return
	}

	ddeal, err := DealFromDB(ctx, m.DB, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if ddeal.Data == nil && deal == nil {
		log.Errorw("cannot finalize deal with missing data source", "deal", id)
		http.Error(w, "cannot finalize deal with missing data source", int(ErrBadProposal))
		return
	}

	var pcidStr string
	var rawSize, refID, pid, pieceSize int64

	err = m.DB.QueryRow(ctx, `SELECT r.ref_id, p.piece_cid, p.piece_padded_size, p.piece_raw_size, p.id
									FROM market_mk20_upload_waiting u
									JOIN parked_piece_refs r ON u.ref_id = r.ref_id
									JOIN parked_pieces p ON r.piece_id = p.id
									WHERE u.id = $1
									  AND p.complete = TRUE
									  AND p.long_term = TRUE;`, id.String()).Scan(&refID, &pcidStr, &pieceSize, &rawSize, &pid)
	if err != nil {
		log.Errorw("failed to get piece details", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}
	pcid, err := cid.Parse(pcidStr)
	if err != nil {
		log.Errorw("failed to parse piece cid", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
	}

	var uDeal *Deal
	var dealUpdated bool

	if deal != nil {
		// This is a deal where DataSource was not set - we should update the deal
		code, ndeal, _, err := m.updateDealDetails(id, deal, auth)
		if err != nil {
			log.Errorw("failed to update deal details", "deal", id, "error", err)
			if code == ErrServerInternalError {
				http.Error(w, "", int(ErrServerInternalError))
			} else {
				http.Error(w, err.Error(), int(code))
			}
			return
		}
		uDeal = ndeal
		dealUpdated = true
	} else {
		uDeal = ddeal
	}

	pi, err := uDeal.PieceInfo()
	if err != nil {
		log.Errorw("failed to get piece info", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if !pi.PieceCIDV1.Equals(pcid) {
		log.Errorw("piece cid mismatch", "deal", id, "expected", pcid, "actual", pi.PieceCIDV1)
		http.Error(w, "piece cid mismatch", int(ErrBadProposal))
		return
	}

	if pi.Size != abi.PaddedPieceSize(pieceSize) {
		log.Errorw("piece size mismatch", "deal", id, "expected", pi.Size, "actual", pieceSize)
		http.Error(w, "piece size mismatch", int(ErrBadProposal))
		return
	}

	if pi.RawSize != uint64(rawSize) {
		log.Errorw("piece raw size mismatch", "deal", id, "expected", pi.RawSize, "actual", rawSize)
		http.Error(w, "piece raw size mismatch", int(ErrBadProposal))
		return
	}

	if uDeal.Products.DDOV1 != nil {
		rej, err := m.sanitizeDDODeal(ctx, uDeal)
		if err != nil {
			log.Errorw("failed to sanitize DDO deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		if rej != nil {
			if rej.HTTPCode == 500 {
				log.Errorw("failed to sanitize DDO deal", "deal", id, "error", rej.Reason)
				http.Error(w, "", int(ErrServerInternalError))
				return
			}
			if rej.HTTPCode != 500 {
				log.Errorw("failed to sanitize DDO deal", "deal", id, "error", rej.Reason)
				http.Error(w, rej.Reason, int(rej.HTTPCode))
				return
			}
		}
	}

	if uDeal.Products.PDPV1 != nil {
		rej, err := m.sanitizePDPDeal(ctx, uDeal)
		if err != nil {
			log.Errorw("failed to sanitize PDP deal", "deal", id, "error", err)
			http.Error(w, "", int(ErrServerInternalError))
			return
		}
		if rej != nil {
			if rej.HTTPCode == 500 {
				log.Errorw("failed to sanitize PDP deal", "deal", id, "error", rej.Reason)
				http.Error(w, "", int(ErrServerInternalError))
				return
			}
			if rej.HTTPCode != 500 {
				log.Errorw("failed to sanitize PDP deal", "deal", id, "error", rej.Reason)
				http.Error(w, rej.Reason, int(rej.HTTPCode))
				return
			}
		}
	}

	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		_, err = tx.Exec(`DELETE FROM market_mk20_upload_waiting WHERE id = $1`, id.String())
		if err != nil {
			return false, xerrors.Errorf("failed to delete upload waiting: %w", err)
		}

		if dealUpdated {
			// Save the updated deal to DB
			err = uDeal.UpdateDeal(tx)
			if err != nil {
				return false, xerrors.Errorf("failed to update deal: %w", err)
			}
		}

		retv := uDeal.Products.RetrievalV1
		data := uDeal.Data

		aggregation := 0
		if data.Format.Aggregate != nil {
			aggregation = int(data.Format.Aggregate.Type)
		}

		var refUsed bool

		if uDeal.Products.DDOV1 != nil {
			ddo := uDeal.Products.DDOV1
			spid, err := address.IDFromAddress(ddo.Provider)
			if err != nil {
				return false, fmt.Errorf("getting provider ID: %w", err)
			}

			pieceIDUrl := url.URL{
				Scheme: "pieceref",
				Opaque: fmt.Sprintf("%d", refID),
			}

			var allocationID interface{}
			if ddo.AllocationId != nil {
				allocationID = *ddo.AllocationId
			} else {
				allocationID = nil
			}

			n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
						   id, sp_id, contract, client, piece_cid_v2, piece_cid,
						   piece_size, raw_size, url, offline, indexing, announce,
						   allocation_id, duration, piece_aggregation, deal_aggregation, started, downloaded, after_commp)
		       		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, TRUE, TRUE, TRUE)`,
				id.String(), spid, ddo.ContractAddress, uDeal.Client, data.PieceCID.String(), pi.PieceCIDV1.String(),
				pi.Size, pi.RawSize, pieceIDUrl.String(), false, retv.Indexing, retv.AnnouncePayload,
				allocationID, ddo.Duration, aggregation, aggregation)

			if err != nil {
				return false, xerrors.Errorf("inserting mk20 pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
			}

			log.Debugw("mk20 pipeline created", "deal", id)

			refUsed = true
		}

		if uDeal.Products.PDPV1 != nil {
			pdp := uDeal.Products.PDPV1
			// Insert the PDP pipeline
			if refUsed {
				err = tx.QueryRow(`
							INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
							VALUES ($1, $2, TRUE) RETURNING ref_id
						`, pid, "/PUT").Scan(&refID)
				if err != nil {
					return false, fmt.Errorf("failed to create parked_piece_refs entry: %w", err)
				}
			}

			n, err := tx.Exec(`INSERT INTO pdp_pipeline (
						id, client, piece_cid_v2, data_set_id, 
						extra_data, piece_ref, downloaded, deal_aggregation, aggr_index, indexing, announce, announce_payload, after_commp) 
					 VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7, 0, $8, $9, $10, TRUE)`,
				id.String(), uDeal.Client, uDeal.Data.PieceCID.String(), *pdp.DataSetID,
				pdp.ExtraData, refID, aggregation, retv.Indexing, retv.AnnouncePiece, retv.AnnouncePayload)
			if err != nil {
				return false, xerrors.Errorf("inserting in PDP pipeline: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("inserting in PDP pipeline: %d rows affected", n)
			}
			log.Debugw("PDP pipeline created", "deal", id)
		}

		return true, nil
	})

	if err != nil {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if !comm {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", "failed to commit transaction")
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	w.WriteHeader(int(Ok))
}

func removeNotFinalizedUploads(ctx context.Context, db *harmonydb.DB) {
	rm := func(ctx context.Context, db *harmonydb.DB) {
		var deals []struct {
			ID      string        `db:"id"`
			Chunked bool          `db:"chunked"`
			RefID   sql.NullInt64 `db:"ref_id"`
			ReadyAt time.Time     `db:"ready_at"`
		}

		err := db.Select(ctx, &deals, `SELECT id, chunked, ref_id, ready_at
											FROM market_mk20_upload_waiting
											WHERE chunked IS NOT NULL
											  AND ready_at <= NOW() AT TIME ZONE 'UTC' - INTERVAL '60 minutes';`)
		if err != nil {
			log.Errorw("failed to get not finalized uploads", "error", err)
		}

		for _, deal := range deals {
			if deal.Chunked {
				comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
					_, err = tx.Exec(`DELETE FROM parked_piece_refs p
											USING (
											  SELECT DISTINCT ref_id
											  FROM market_mk20_deal_chunk
											  WHERE id = $1 AND ref_id IS NOT NULL
											) c
											WHERE p.ref_id = c.ref_id;
											`, deal.ID)
					if err != nil {
						return false, xerrors.Errorf("deleting piece refs: %w", err)
					}

					_, err = tx.Exec(`DELETE FROM market_mk20_deal_chunk WHERE id = $1`, deal.ID)
					if err != nil {
						return false, xerrors.Errorf("deleting deal chunks: %w", err)
					}

					n, err := tx.Exec(`UPDATE market_mk20_upload_waiting
											SET chunked = NULL,
												ref_id  = NULL,
												ready_at = NULL
											WHERE id = $1;`, deal.ID)

					if err != nil {
						return false, xerrors.Errorf("updating upload waiting: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("updating upload waiting: expected 1 row updated, got %d", n)
					}

					return true, nil
				}, harmonydb.OptionRetry())
				if err != nil {
					log.Errorw("failed to delete not finalized uploads", "deal", deal.ID, "error", err)
				}
				if !comm {
					log.Errorw("failed to delete not finalized uploads", "deal", deal.ID, "error", "failed to commit transaction")
				}
			} else {
				if deal.RefID.Valid {
					comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
						_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, deal.RefID.Int64)
						if err != nil {
							return false, xerrors.Errorf("deleting piece refs: %w", err)
						}

						n, err := tx.Exec(`UPDATE market_mk20_upload_waiting
											SET chunked = NULL,
												ref_id  = NULL,
												ready_at = NULL
											WHERE id = $1;`, deal.ID)

						if err != nil {
							return false, xerrors.Errorf("updating upload waiting: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("updating upload waiting: expected 1 row updated, got %d", n)
						}

						return true, nil
					})
					if err != nil {
						log.Errorw("failed to delete not finalized uploads", "deal", deal.ID, "error", err)
					}
					if !comm {
						log.Errorw("failed to delete not finalized uploads", "deal", deal.ID, "error", "failed to commit transaction")
					}
				}
				log.Errorw("removing not finalized upload", "deal", deal.ID, "error", "ref_id not found")
			}
		}
	}

	ticker := time.NewTicker(time.Minute * 5)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rm(ctx, db)
		case <-ctx.Done():
			return
		}
	}
}
