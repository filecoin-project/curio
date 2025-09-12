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

// DealStatus retrieves the status of a specific deal by querying the database and determining the current state for both PDP and DDO processing.
// @param id [ulid.ULID]
// @Return http.StatusNotFound
// @Return http.StatusInternalServerError
// @Return *DealProductStatusResponse

func (m *MK20) DealStatus(ctx context.Context, id ulid.ULID) *DealStatus {
	var pdp_complete, ddo_complete sql.NullBool
	var pdp_error, ddo_error sql.NullString

	err := m.DB.QueryRow(ctx, `SELECT
									  (pdp_v1->>'complete')::boolean AS pdp_complete,
									  (pdp_v1->>'error')::text AS pdp_error,
									  (ddo_v1->>'complete')::boolean AS ddo_complete,
									  (ddo_v1->>'error')::text AS ddo_error
									FROM market_mk20_deal
									WHERE id = $1;`, id.String()).Scan(&pdp_complete, &pdp_error, &ddo_complete, &ddo_error)
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

	deal, err := DealFromDB(ctx, m.DB, id)
	if err != nil {
		log.Errorw("failed to get deal from db", "deal", id, "error", err)
		return &DealStatus{
			HTTPCode: http.StatusInternalServerError,
		}
	}

	isPDP := deal.Products.PDPV1 != nil
	isDDO := deal.Products.DDOV1 != nil

	// If only PDP is defined
	if isPDP && !isDDO {
		ret := &DealStatus{
			HTTPCode: http.StatusOK,
			Response: &DealProductStatusResponse{
				PDPV1: &DealStatusResponse{
					State: DealStateAccepted,
				},
			},
		}
		if pdp_complete.Bool {
			ret.Response.PDPV1.State = DealStateComplete
		}
		if pdp_error.Valid && pdp_error.String != "" {
			ret.Response.PDPV1.State = DealStateFailed
			ret.Response.PDPV1.ErrorMsg = pdp_error.String
		}

		if !pdp_complete.Bool {
			pdp := deal.Products.PDPV1
			if pdp.AddPiece {
				if deal.Data != nil {
					// Check if deal is uploaded
					var yes bool
					err = m.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_upload_waiting WHERE id = $1)`, id.String()).Scan(&yes)
					if err != nil {
						log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
						return &DealStatus{
							HTTPCode: http.StatusInternalServerError,
						}
					}
					if yes {
						ret.Response.PDPV1.State = DealStateAwaitingUpload
					} else {
						ret.Response.PDPV1.State = DealStateProcessing
					}
				} else {
					ret.Response.PDPV1.State = DealStateAccepted
				}
			}

			if pdp.CreateDataSet || pdp.DeleteDataSet || pdp.DeletePiece {
				ret.Response.PDPV1.State = DealStateProcessing
			}
		}

		return ret
	}

	// If only DDO is defined
	if isDDO && !isPDP {
		ret := &DealStatus{
			HTTPCode: http.StatusOK,
			Response: &DealProductStatusResponse{
				DDOV1: &DealStatusResponse{
					State: DealStateAccepted,
				},
			},
		}
		if ddo_complete.Bool {
			ret.Response.DDOV1.State = DealStateComplete
		}
		if ddo_error.Valid && ddo_error.String != "" {
			ret.Response.DDOV1.State = DealStateFailed
			ret.Response.DDOV1.ErrorMsg = ddo_error.String
		}

		if !ddo_complete.Bool {
			state, err := m.getDDOStatus(ctx, id)
			if err != nil {
				log.Errorw("failed to get DDO status", "deal", id.String(), "error", err)
				return &DealStatus{
					HTTPCode: http.StatusInternalServerError,
				}
			}
			ret.Response.DDOV1.State = state
		}

		return ret
	}

	// If both PDP and DDO are defined
	if isPDP && isDDO {
		ret := &DealStatus{
			HTTPCode: http.StatusOK,
		}

		if pdp_complete.Bool {
			ret.Response.PDPV1.State = DealStateComplete
		}

		if pdp_error.Valid {
			ret.Response.PDPV1.State = DealStateFailed
			ret.Response.PDPV1.ErrorMsg = pdp_error.String
		}

		if ddo_complete.Bool {
			ret.Response.DDOV1.State = DealStateComplete
		}

		if ddo_error.Valid && ddo_error.String != "" {
			ret.Response.DDOV1.State = DealStateFailed
			ret.Response.DDOV1.ErrorMsg = ddo_error.String
		}

		if !pdp_complete.Bool {
			pdp := deal.Products.PDPV1
			if pdp.AddPiece {
				if deal.Data != nil {
					// Check if deal is uploaded
					var yes bool
					err = m.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_upload_waiting WHERE id = $1)`, id.String()).Scan(&yes)
					if err != nil {
						log.Errorw("failed to query the db for deal status", "deal", id.String(), "err", err)
						return &DealStatus{
							HTTPCode: http.StatusInternalServerError,
						}
					}
					if yes {
						ret.Response.PDPV1.State = DealStateAwaitingUpload
					} else {
						ret.Response.PDPV1.State = DealStateProcessing
					}
				} else {
					ret.Response.PDPV1.State = DealStateAccepted
				}
			}

			if pdp.CreateDataSet || pdp.DeleteDataSet || pdp.DeletePiece {
				ret.Response.PDPV1.State = DealStateProcessing
			}
		}

		if !ddo_complete.Bool {
			state, err := m.getDDOStatus(ctx, id)
			if err != nil {
				log.Errorw("failed to get DDO status", "deal", id.String(), "error", err)
				return &DealStatus{
					HTTPCode: http.StatusInternalServerError,
				}
			}
			ret.Response.DDOV1.State = state
		}

		return ret
	}

	return &DealStatus{
		HTTPCode: http.StatusInternalServerError,
	}

}

func (m *MK20) getDDOStatus(ctx context.Context, id ulid.ULID) (DealState, error) {
	var waitingForPipeline bool
	err := m.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM market_mk20_pipeline_waiting WHERE id = $1)`, id.String()).Scan(&waitingForPipeline)
	if err != nil {
		return DealStateAccepted, err
	}
	if waitingForPipeline {
		return DealStateAccepted, nil
	}

	var pdeals []struct {
		Sector  *int `db:"sector"`
		Sealed  bool `db:"sealed"`
		Indexed bool `db:"indexed"`
	}

	err = m.DB.Select(ctx, &pdeals, `SELECT 
									sector,
									sealed,
									indexed
								FROM 
									market_mk20_pipeline
								WHERE 
									id = $1`, id.String())

	if err != nil {
		return DealStateAccepted, err
	}

	if len(pdeals) > 1 {
		return DealStateProcessing, nil
	}

	// If deal is still in pipeline
	if len(pdeals) == 1 {
		pdeal := pdeals[0]
		if pdeal.Sector == nil {
			return DealStateProcessing, nil
		}
		if !pdeal.Sealed {
			return DealStateSealing, nil
		}
		if !pdeal.Indexed {
			return DealStateIndexing, nil
		}
	}

	return DealStateComplete, nil
}

// Supported retrieves and returns maps of product names and data source names with their enabled status, or an error if the query fails.
func (m *MK20) Supported(ctx context.Context) (map[string]bool, map[string]bool, error) {
	var products []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	err := m.DB.Select(ctx, &products, `SELECT name, enabled FROM market_mk20_products`)
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
	err = m.DB.Select(ctx, &sources, `SELECT name, enabled FROM market_mk20_data_source`)
	if err != nil {
		return nil, nil, err
	}
	sourcesMap := make(map[string]bool)
	for _, source := range sources {
		sourcesMap[source.Name] = source.Enabled
	}
	return productsMap, sourcesMap, nil
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

const UploadSizeLimit = int64(1 * 1024 * 1024 * 1024)

func (t *TimeoutLimitReader) Read(p []byte) (int, error) {
	deadline := time.Now().Add(t.timeout)
	for {
		// Attempt to read
		n, err := t.r.Read(p)
		if t.totalBytes+int64(n) > UploadSizeLimit {
			return 0, fmt.Errorf("upload size limit exceeded: %d bytes", UploadSizeLimit)
		} else {
			t.totalBytes += int64(n)
		}

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
