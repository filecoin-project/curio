package pdp

import (
	"context"
	"errors"
	"fmt"

	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var (
	// ErrDataSetNotFound indicates the data set does not exist or does not belong to the service.
	ErrDataSetNotFound = errors.New("data set not found")
	// ErrDataSetTerminated indicates the data set was terminated due to unrecoverable proving failure.
	ErrDataSetTerminated = errors.New("data set has been terminated due to unrecoverable proving failure")
)

// verifyDataSetForService checks that dataSetId exists in pdp_data_sets, belongs to service,
// and has not been terminated due to unrecoverable proving failure.
func verifyDataSetForService(ctx context.Context, db *harmonydb.DB, service string, dataSetId uint64) error {
	var dataSetService string
	var unrecoverable *int64
	err := db.QueryRow(ctx, `
		SELECT service, unrecoverable_proving_failure_epoch
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetId).Scan(&dataSetService, &unrecoverable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDataSetNotFound
		}
		return fmt.Errorf("failed to retrieve data set: %w", err)
	}

	if dataSetService != service {
		return ErrDataSetNotFound
	}

	if unrecoverable != nil {
		return ErrDataSetTerminated
	}

	return nil
}
