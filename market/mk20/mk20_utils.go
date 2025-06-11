package mk20

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
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
