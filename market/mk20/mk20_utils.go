package mk20

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/dealdata"
)

func (m *MK20) DealStatus(ctx context.Context, id ulid.ULID) *DealStatus {
	// Check if we ever accepted this deal

	var dealError sql.NullString

	err := m.db.QueryRow(ctx, `SELECT error FROM market_mk20_pipeline WHERE id = $1)`, id.String()).Scan(&dealError)
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

	var waitingDeal []struct {
		Started   bool       `db:"started_put"`
		StartTime *time.Time `db:"start_time"`
	}

	err := m.db.Select(ctx, &waitingDeal, `SELECT started_put, start_time from market_mk20_pipeline_waiting 
                                    WHERE waiting_for_data = TRUE AND id = $1)`, id.String())

	if err != nil {
		log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if len(waitingDeal) == 0 {
		http.Error(w, "", http.StatusNotFound)
	}

	if waitingDeal[0].Started && waitingDeal[0].StartTime.Add(m.cfg.HTTP.ReadTimeout).Before(time.Now()) {
		http.Error(w, "another /PUT request is in progress for this deal", http.StatusConflict)
	}

	// TODO: Rethink how to ensure only 1 process per deal for /PUT

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
		if failed {
			_, err = m.db.Exec(ctx, `UPDATE market_mk20_pipeline_waiting SET started_put = FALSE, start_time = NULL WHERE id = $1`, id.String())
			if err != nil {
				log.Errorw("failed to update deal status in db", "deal", id.String(), "err", err)
			}
		}
	}()

	// Function to write data into StashStore and calculate commP
	writeFunc := func(f *os.File) error {
		limitedReader := io.LimitReader(data, int64(rawSize+1)) // +1 to detect exceeding the limit

		n, err := io.Copy(f, limitedReader)
		if err != nil {
			return fmt.Errorf("failed to read and write piece data: %w", err)
		}

		if n > int64(deal.Data.Size) {
			return fmt.Errorf("piece data exceeds the maximum allowed size")
		}

		if int64(rawSize) != n {
			return fmt.Errorf("raw size does not match with uploaded data: %w", err)
		}

		return nil
	}

	// Upload into StashStore
	stashID, err := m.stor.StashCreate(ctx, int64(deal.Data.Size), writeFunc)
	if err != nil {
		if err.Error() == "piece data exceeds the maximum allowed size" {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		} else if err.Error() == "raw size does not match with uploaded data" {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
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

		var aggregation interface{}
		if dealdata.Format.Aggregate != nil {
			aggregation = dealdata.Format.Aggregate.Type
		} else {
			aggregation = nil
		}

		n, err = tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid,
            piece_size, raw_size, offline, indexing, announce,
            allocation_id, duration, piece_aggregation, started) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, TRUE)`,
			dealID, spid, ddo.ContractAddress, ddo.Client.String(), dealdata.PieceCID.String(),
			dealdata.Size, int64(dealdata.SourceHTTP.RawSize), false, ddo.Indexing, ddo.AnnounceToIPNI,
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

	if err != nil || !comm {
		// Remove the stash file as the transaction failed
		_ = m.stor.StashRemove(ctx, stashID)
		http.Error(w, "Failed to process piece upload", http.StatusInternalServerError)
		return
	}

	failed = false
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
