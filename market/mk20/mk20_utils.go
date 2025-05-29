package mk20

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/dealdata"
)

func (m *MK20) DealStatus(ctx context.Context, id ulid.ULID) *DealStatus {
	// Check if we ever accepted this deal

	var dealError sql.NullString

	err := m.db.QueryRow(ctx, `SELECT error FROM market_mk20_deal WHERE id = $1;`, id.String()).Scan(&dealError)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &DealStatus{
				HTTPCode: http.StatusNotFound,
			}
		}
		log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
		return &DealStatus{
			HTTPCode: http.StatusInternalServerError,
		}
	}
	if dealError.Valid {
		return &DealStatus{
			HTTPCode: http.StatusOK,
			Response: &DealStatusResponse{
				State:    DealStateFailed,
				ErrorMsg: dealError.String,
			},
		}
	}

	var waitingForPipeline bool
	err = m.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_pipeline_waiting WHERE id = $1)`, id.String()).Scan(&waitingForPipeline)
	if err != nil {
		log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
		return &DealStatus{
			HTTPCode: http.StatusInternalServerError,
		}
	}
	if waitingForPipeline {
		return &DealStatus{
			HTTPCode: http.StatusOK,
			Response: &DealStatusResponse{
				State: DealStateAccepted,
			},
		}
	}

	var pdeals []struct {
		Sector  *int `db:"sector"`
		Sealed  bool `db:"sealed"`
		Indexed bool `db:"indexed"`
	}

	err = m.db.Select(ctx, &pdeals, `SELECT 
									sector,
									sealed,
									indexed
								FROM 
									market_mk20_pipeline
								WHERE 
									id = $1`, id.String())

	if err != nil {
		log.Errorw("failed to query the db for deal pipeline status", "deal", id.String(), "err", err)
		return &DealStatus{
			HTTPCode: http.StatusInternalServerError,
		}
	}

	if len(pdeals) > 1 {
		return &DealStatus{
			HTTPCode: http.StatusOK,
			Response: &DealStatusResponse{
				State: DealStateProcessing,
			},
		}
	}

	// If deal is still in pipeline
	if len(pdeals) == 1 {
		pdeal := pdeals[0]
		if pdeal.Sector == nil {
			return &DealStatus{
				HTTPCode: http.StatusOK,
				Response: &DealStatusResponse{
					State: DealStateProcessing,
				},
			}
		}
		if !pdeal.Sealed {
			return &DealStatus{
				HTTPCode: http.StatusOK,
				Response: &DealStatusResponse{
					State: DealStateSealing,
				},
			}
		}
		if !pdeal.Indexed {
			return &DealStatus{
				HTTPCode: http.StatusOK,
				Response: &DealStatusResponse{
					State: DealStateIndexing,
				},
			}
		}
	}

	return &DealStatus{
		HTTPCode: http.StatusOK,
		Response: &DealStatusResponse{
			State: DealStateComplete,
		},
	}
}

func (m *MK20) HandlePutRequest(id ulid.ULID, data io.ReadCloser, w http.ResponseWriter) {
	ctx := context.Background()
	defer data.Close()

	var (
		idStr   string
		updated bool
	)

	err := m.db.QueryRow(ctx, `WITH check_row AS (
									  SELECT id FROM market_mk20_pipeline_waiting 
									  WHERE id = $1 AND waiting_for_data = TRUE
									),
									try_update AS (
									  UPDATE market_mk20_pipeline_waiting
									  SET start_time = NOW()
									  WHERE id IN (SELECT id FROM check_row)
										AND (
										  start_time IS NULL
										  OR start_time < NOW() - ($2 * INTERVAL '1 second')
										)
									  RETURNING id
									)
									SELECT 
									  check_row.id, 
									  try_update.id IS NOT NULL AS updated
									FROM check_row
									LEFT JOIN try_update ON check_row.id = try_update.id;`,
		id.String(), m.cfg.HTTP.ReadTimeout.Seconds()).Scan(&idStr, &updated)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "", http.StatusNotFound)
			return
		}
		log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if idStr != id.String() {
		log.Errorw("deal id mismatch", "deal", id.String(), "db", idStr)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if !updated {
		http.Error(w, "", http.StatusConflict)
	}

	deal, err := DealFromDB(ctx, m.db, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	rawSize, err := deal.Data.RawSize()
	if err != nil {
		log.Errorw("failed to get raw size of deal", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	n, err := m.db.Exec(ctx, `UPDATE market_mk20_pipeline_waiting SET started_put = TRUE, start_time = NOW() WHERE id = $1`, id.String())
	if err != nil {
		log.Errorw("failed to update deal status in db", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if n != 1 {
		log.Errorw("failed to update deal status in db", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	failed := true

	defer func() {
		m.maxParallelUploads.Add(-1)
		if failed {
			_, err = m.db.Exec(ctx, `UPDATE market_mk20_pipeline_waiting SET started_put = FALSE, start_time = NULL WHERE id = $1`, id.String())
			if err != nil {
				log.Errorw("failed to update deal status in db", "deal", id.String(), "err", err)
			}
		}
	}()

	if m.maxParallelUploads.Load() >= int32(m.cfg.Market.StorageMarketConfig.MK20.MaxParallelUploads) {
		http.Error(w, "Too many parallel uploads", http.StatusTooManyRequests)
		return
	}

	m.maxParallelUploads.Add(1)

	cp := new(commp.Calc)
	reader := NewTimeoutReader(data, time.Second*5)

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitedReader := io.LimitReader(reader, int64(rawSize+1)) // +1 to detect exceeding the limit
		wr := io.MultiWriter(f, cp)

		size, err := io.CopyBuffer(wr, limitedReader, make([]byte, 4<<20))
		if err != nil {
			return fmt.Errorf("failed to read and write piece data: %w", err)
		}

		if size > int64(deal.Data.Size) {
			return fmt.Errorf("piece data exceeds the maximum allowed size")
		}

		if int64(rawSize) != size {
			return fmt.Errorf("deal raw size %d does not match with uploaded data size %d", rawSize, size)
		}

		digest, pieceSize, err := cp.Digest()
		if err != nil {
			return fmt.Errorf("failed to calculate commP: %w", err)
		}

		pieceCIDComputed, err := commcid.DataCommitmentV1ToCID(digest)
		if err != nil {
			return fmt.Errorf("failed to calculate piece CID: %w", err)
		}
		if !pieceCIDComputed.Equals(deal.Data.PieceCID) {
			return fmt.Errorf("calculated piece CID %s does not match with deal piece CID %s", pieceCIDComputed.String(), deal.Data.PieceCID.String())
		}

		if abi.PaddedPieceSize(pieceSize) != deal.Data.Size {
			return fmt.Errorf("calculated piece size %d does not match with deal piece size %d", pieceSize, deal.Data.Size)
		}

		return nil
	}

	// Upload into StashStore
	stashID, err := m.stor.StashCreate(ctx, int64(deal.Data.Size), writeFunc)
	if err != nil {
		if err.Error() == "piece data exceeds the maximum allowed size" {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, "piece data exceeds the maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		} else if strings.Contains(err.Error(), "does not match with uploaded data") {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, errors.Unwrap(err).Error(), http.StatusBadRequest)
			return
		} else if strings.Contains(err.Error(), "failed to calculate piece CID") {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, "Failed to calculate piece CID", http.StatusInternalServerError)
			return
		} else if strings.Contains(err.Error(), "calculated piece CID does not match with uploaded data") {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, errors.Unwrap(err).Error(), http.StatusBadRequest)
			return
		} else if strings.Contains(err.Error(), "calculated piece size does not match with uploaded data") {
			log.Errorw("Storing", "Deal", id, "error", err)
			http.Error(w, errors.Unwrap(err).Error(), http.StatusBadRequest)
			return
		} else {
			log.Errorw("Failed to store piece data in StashStore", "error", err)
			http.Error(w, "Failed to store piece data", http.StatusInternalServerError)
			return
		}
	}

	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {

		// 1. Create a long-term parked piece entry
		var parkedPieceID int64
		err := tx.QueryRow(`
            INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
            VALUES ($1, $2, $3, TRUE) RETURNING id
        `, deal.Data.PieceCID.String(), deal.Data.Size, rawSize).Scan(&parkedPieceID)
		if err != nil {
			return false, fmt.Errorf("failed to create parked_pieces entry: %w", err)
		}

		// 2. Create a piece ref with data_url being "stashstore://<stash-url>"
		// Get StashURL
		stashURL, err := m.stor.StashURL(stashID)
		if err != nil {
			return false, fmt.Errorf("failed to get stash URL: %w", err)
		}

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

		n, err := tx.Exec(`INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids) VALUES ($1, $2, $3, $4)`,
			id.String(), deal.Data.PieceCID.String(), deal.Data.Size, []int64{pieceRefID})
		if err != nil {
			return false, xerrors.Errorf("inserting mk20 download pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("inserting mk20 download pipeline: %d rows affected", n)
		}

		spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
		if err != nil {
			return false, fmt.Errorf("getting provider ID: %w", err)
		}

		ddo := deal.Products.DDOV1
		dealdata := deal.Data
		dealID := deal.Identifier.String()

		var allocationID interface{}
		if ddo.AllocationId != nil {
			allocationID = *ddo.AllocationId
		} else {
			allocationID = nil
		}

		aggregation := 0
		if dealdata.Format.Aggregate != nil {
			aggregation = int(dealdata.Format.Aggregate.Type)
		}

		n, err = tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid,
            piece_size, raw_size, offline, indexing, announce,
            allocation_id, duration, piece_aggregation, started, after_commp) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, TRUE, TRUE)`,
			dealID, spid, ddo.ContractAddress, ddo.Client.String(), dealdata.PieceCID.String(),
			dealdata.Size, int64(dealdata.SourceHttpPut.RawSize), false, ddo.Indexing, ddo.AnnounceToIPNI,
			allocationID, ddo.Duration, aggregation)
		if err != nil {
			return false, xerrors.Errorf("inserting mk20 pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
		}

		_, err = tx.Exec(`DELETE FROM market_mk20_pipeline_waiting WHERE id = $1`, id.String())
		if err != nil {
			return false, xerrors.Errorf("deleting deal from mk20 pipeline waiting: %w", err)
		}

		return true, nil // Commit the transaction
	}, harmonydb.OptionRetry())

	if err != nil {
		log.Errorw("Failed to process piece upload", "Deal", id, "error", err)
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		err = m.stor.StashRemove(ctx, stashID)
		if err != nil {
			log.Errorw("Failed to remove stash file", "Deal", id, "error", err)
		}
		return
	}

	if !comm {
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		err = m.stor.StashRemove(ctx, stashID)
		if err != nil {
			log.Errorw("Failed to remove stash file", "Deal", id, "error", err)
		}
		return
	}

	failed = false
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (m *MK20) Supported(ctx context.Context) (map[string]bool, map[string]bool, error) {
	var products []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	err := m.db.Select(ctx, &products, `SELECT name, enabled FROM market_mk20_products`)
	if err != nil {
		return nil, nil, err
	}

	productsMap := make(map[string]bool)

	for _, product := range products {
		productsMap[product.Name] = product.Enabled
	}

	var sources []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	err = m.db.Select(ctx, &sources, `SELECT name, enabled FROM market_mk20_data_source`)
	if err != nil {
		return nil, nil, err
	}
	sourcesMap := make(map[string]bool)
	for _, source := range sources {
		sourcesMap[source.Name] = source.Enabled
	}
	return productsMap, sourcesMap, nil
}

type TimeoutReader struct {
	r       io.Reader
	timeout time.Duration
}

func NewTimeoutReader(r io.Reader, timeout time.Duration) *TimeoutReader {
	return &TimeoutReader{
		r:       r,
		timeout: timeout,
	}
}

func (t *TimeoutReader) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.timeout)
	for {
		// Attempt to read
		n, err := t.r.Read(p)

		if err != nil {
			return n, err
		}

		if n > 0 {
			// Successfully read some data; reset the deadline
			deadline = time.Now().Add(t.timeout)

			// Otherwise return bytes read and no error
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
