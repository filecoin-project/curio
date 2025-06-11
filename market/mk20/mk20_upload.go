package mk20

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/dealdata"
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

	rawSize, err := deal.Data.RawSize()
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
		Chunk    int   `db:"chunk"`
		Size     int64 `db:"chunk_size"`
		Complete bool  `db:"complete"`
	}
	err := m.db.Select(ctx, &chunkDetails, `SELECT chunk, chunk_size, complete 
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

	defer func() {
		m.maxParallelUploads.Add(-1)
	}()

	log.Debugw("uploading chunk", "deal", id, "chunk", chunk)

	chunkSize := chunkDetails[0].Size
	reader := NewTimeoutReader(data, time.Second*5)
	m.maxParallelUploads.Add(1)

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitedReader := io.LimitReader(reader, chunkSize+1) // +1 to detect exceeding the limit

		size, err := io.CopyBuffer(f, limitedReader, make([]byte, 4<<20))
		if err != nil {
			return fmt.Errorf("failed to read and write chunk data: %w", err)
		}

		if size > chunkSize {
			return fmt.Errorf("chunk data exceeds the maximum allowed size")
		}

		if chunkSize != size {
			return fmt.Errorf("chunk size %d does not match with uploaded data size %d", chunkSize, size)
		}

		return nil
	}

	// Upload into StashStore
	stashID, err := m.stor.StashCreate(ctx, chunkSize, writeFunc)
	if err != nil {
		if err.Error() == "chunk data exceeds the maximum allowed size" {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, "chunk data exceeds the maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		} else if strings.Contains(err.Error(), "does not match with uploaded data") {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, errors.Unwrap(err).Error(), http.StatusBadRequest)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			http.Error(w, "Failed to store piece data", http.StatusInternalServerError)
			return
		}
	}

	log.Debugw("uploaded chunk", "deal", id, "chunk", chunk, "stashID", stashID.String())

	stashUrl, err := m.stor.StashURL(stashID)
	if err != nil {
		log.Errorw("Failed to get stash url", "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	stashUrl.Scheme = dealdata.CustoreScheme

	log.Debugw("uploading chunk generated URL", "deal", id, "chunk", chunk, "url", stashUrl.String())

	n, err := m.db.Exec(ctx, `UPDATE market_mk20_deal_chunk SET complete = TRUE, 
                                  url = $1 
                              WHERE id = $2 
                                AND chunk = $3
                                AND complete = FALSE`, stashUrl.String(), id.String(), chunk)
	if err != nil {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		err = m.stor.StashRemove(ctx, stashID)
		if err != nil {
			log.Errorw("Failed to remove stash file", "Deal", id, "error", err)
		}
		return
	}

	if n == 0 {
		log.Errorw("failed to update chunk", "deal", id, "chunk", chunk, "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		err = m.stor.StashRemove(ctx, stashID)
		if err != nil {
			log.Errorw("Failed to remove stash file", "Deal", id, "error", err)
		}
		return
	}

}

func (m *MK20) HandleUploadFinalize(id ulid.ULID, w http.ResponseWriter) {
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
