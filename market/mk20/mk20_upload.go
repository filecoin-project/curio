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
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (m *MK20) HandleUploadStatus(ctx context.Context, id ulid.ULID, w http.ResponseWriter) {
	var exists bool
	err := m.db.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_pipeline_waiting
								  WHERE id = $1 AND waiting_for_data = TRUE
								)`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if upload is waiting for data", "deal", id, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}

	var ret UploadStatus

	err = m.db.QueryRow(ctx, `SELECT
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
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		http.Error(w, "chunk size not updated", http.StatusNotFound)
		return
	}

	data, err := json.Marshal(ret)
	if err != nil {
		log.Errorw("failed to marshal upload status", "deal", id, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(data)
	if err != nil {
		log.Errorw("failed to write upload status", "deal", id, "error", err)
	}
}

func (m *MK20) HandleUploadStart(ctx context.Context, id ulid.ULID, chunkSize int64, w http.ResponseWriter) {
	if chunkSize == 0 {
		log.Errorw("chunk size must be greater than 0", "id", id)
		http.Error(w, "chunk size must be greater than 0", http.StatusBadRequest)
		return
	}

	// Check if chunk size is a power of 2
	if chunkSize&(chunkSize-1) != 0 {
		log.Errorw("chunk size must be a power of 2", "id", id)
		http.Error(w, "chunk size must be a power of 2", http.StatusBadRequest)
		return
	}

	// Check that chunk size align with config
	if chunkSize < m.cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize {
		log.Errorw("chunk size too small", "id", id)
		http.Error(w, "chunk size too small", http.StatusBadRequest)
		return
	}
	if chunkSize > m.cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize {
		log.Errorw("chunk size too large", "id", id)
		http.Error(w, "chunk size too large", http.StatusBadRequest)
		return
	}

	// Check if deal exists
	var exists bool

	err := m.db.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_deal
								  WHERE id = $1
								);`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if deal exists", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "deal not found", http.StatusNotFound)
		return
	}

	// Check if we already started the upload
	var started bool
	err = m.db.QueryRow(ctx, `SELECT EXISTS (
									SELECT 1 
									FROM market_mk20_deal_chunk 
									WHERE id = $1);`, id.String()).Scan(&started)
	if err != nil {
		log.Errorw("failed to check if deal upload has started", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if started {
		http.Error(w, "deal upload has already started", http.StatusTooManyRequests)
		return
	}

	deal, err := DealFromDB(ctx, m.db, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	rawSize, err := deal.RawSize()
	if err != nil {
		log.Errorw("failed to get raw size of deal", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	numChunks := int(math.Ceil(float64(rawSize) / float64(chunkSize)))

	// Create rows in market_mk20_deal_chunk for each chunk for the ID
	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
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
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("failed to create chunks for deal", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	if !comm {
		log.Errorw("failed to create chunks for deal", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (m *MK20) HandleUploadChunk(id ulid.ULID, chunk int, data io.ReadCloser, w http.ResponseWriter) {
	ctx := context.Background()
	defer data.Close()

	if chunk < 1 {
		http.Error(w, "chunk must be greater than 0", http.StatusBadRequest)
		return
	}

	var chunkDetails []struct {
		Chunk    int           `db:"chunk"`
		Size     int64         `db:"chunk_size"`
		Complete bool          `db:"complete"`
		RefID    sql.NullInt64 `db:"ref_id"`
	}
	err := m.db.Select(ctx, &chunkDetails, `SELECT chunk, chunk_size, ref_id, complete 
								  FROM market_mk20_deal_chunk
								  WHERE id = $1 AND chunk = $2`, id.String(), chunk)
	if err != nil {
		log.Errorw("failed to check if chunk exists", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
	}

	if len(chunkDetails) == 0 {
		http.Error(w, "chunk not found", http.StatusNotFound)
		return
	}

	if len(chunkDetails) > 1 {
		log.Errorw("chunk exists multiple times", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if chunkDetails[0].Complete {
		http.Error(w, "chunk already uploaded", http.StatusConflict)
		return
	}

	if chunkDetails[0].RefID.Valid {
		http.Error(w, "chunk already uploaded", http.StatusConflict)
		return
	}

	defer func() {
		m.maxParallelUploads.Add(-1)
	}()

	log.Debugw("uploading chunk", "deal", id, "chunk", chunk)

	chunkSize := chunkDetails[0].Size
	reader := NewTimeoutReader(data, time.Second*5)
	m.maxParallelUploads.Add(1)

	// Generate unique tmp pieceCID and Size for parked_pieces tables
	wr := new(commp.Calc)
	n, err := wr.Write([]byte(fmt.Sprintf("%s, %d, %d, %s", id.String(), chunk, chunkSize, time.Now().String())))
	if err != nil {
		log.Errorw("failed to generate unique tmp pieceCID and Size for parked_pieces tables", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	digest, tsize, err := wr.Digest()
	if err != nil {
		panic(err)
	}

	tpcid := cid.NewCidV1(cid.FilCommitmentUnsealed, digest)
	var pnum, refID int64

	// Generate piece park details with tmp pieceCID and Size
	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
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
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", "failed to commit transaction")
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	log.Debugw("tmp piece details generated for the chunk", "deal", id, "chunk", chunk)

	failed := true
	defer func() {
		if failed {
			_, err = m.db.Exec(ctx, `DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
			if err != nil {
				log.Errorw("failed to delete parked piece ref", "deal", id, "chunk", chunk, "error", err)
			}
		}
	}()

	// Store the piece and generate PieceCID and Size
	pi, err := m.sc.WriteUploadPiece(ctx, storiface.PieceNumber(pnum), chunkSize, reader, storiface.PathSealing)
	if err != nil {
		log.Errorw("failed to write piece", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	log.Debugw("piece stored", "deal", id, "chunk", chunk)

	// Update piece park details with correct values
	comm, err = m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
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
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if !comm {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", "failed to commit transaction")
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	log.Debugw("chunk upload finished", "deal", id, "chunk", chunk)

	failed = false

}

func (m *MK20) HandleUploadFinalize(id ulid.ULID, deal *Deal, w http.ResponseWriter) {
	ctx := context.Background()
	var exists bool
	err := m.db.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_deal_chunk
								  WHERE id = $1 AND complete = FALSE OR complete IS NULL
								)`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if deal upload has started", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if exists {
		http.Error(w, "deal upload has not finished", http.StatusBadRequest)
		return
	}

	if deal != nil {
		// This is a deal where DataSource was not set - we should update the deal
		code, err := m.updateDealDetails(id, deal)
		if err != nil {
			log.Errorw("failed to update deal details", "deal", id, "error", err)
			if code == http.StatusInternalServerError {
				http.Error(w, "", http.StatusInternalServerError)
			} else {
				http.Error(w, err.Error(), int(code))
			}
			return
		}
	}

	// Now update the upload status to trigger the correct pipeline
	n, err := m.db.Exec(ctx, `UPDATE market_mk20_deal_chunk SET finalize = TRUE where id = $1`, id.String())
	if err != nil {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if n == 0 {
		log.Errorw("failed to finalize deal upload", "deal", id, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (m *MK20) updateDealDetails(id ulid.ULID, deal *Deal) (ErrorCode, error) {
	ctx := context.Background() // Let's not use request context to avoid DB inconsistencies

	if deal.Identifier.Compare(id) != 0 {
		return ErrBadProposal, xerrors.Errorf("deal ID and proposal ID do not match")
	}

	// Validate the deal
	code, err := deal.Validate(m.db, &m.cfg.Market.StorageMarketConfig.MK20)
	if err != nil {
		return code, err
	}

	log.Debugw("deal validated", "deal", deal.Identifier.String())

	// Verify we have a deal is DB
	var exists bool
	err = m.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_deal WHERE id = $1)`, id.String()).Scan(&exists)
	if err != nil {
		return http.StatusInternalServerError, xerrors.Errorf("failed to check if deal exists: %w", err)
	}

	if !exists {
		return http.StatusNotFound, xerrors.Errorf("deal not found")
	}

	// Get updated deal
	ndeal, code, err := UpdateDealDetails(ctx, m.db, id, deal, &m.cfg.Market.StorageMarketConfig.MK20)
	if err != nil {
		return code, err
	}

	// Save the updated deal to DB
	err = ndeal.UpdateDeal(ctx, m.db)
	if err != nil {
		return http.StatusInternalServerError, xerrors.Errorf("failed to update deal: %w", err)
	}
	return http.StatusOK, nil
}
