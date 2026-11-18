package pdpv0

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

type provingAlert int

const (
	provingAlertNone provingAlert = iota
	provingAlertSendError
	provingAlertProvingStopped
)

// markDatasetProvingUnrecoverable stops proving for a data set that can no
// longer be proven (for example after DataSetPaymentBeyondEndEpoch). It reports
// whether the operator still has to be told: this call set the mark and nobody
// has requested the data set's deletion.
func markDatasetProvingUnrecoverable(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) (alert bool, err error) {
	marked, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET unrecoverable_proving_failure_epoch = $2,
			init_ready = FALSE,
			prove_at_epoch = NULL,
			challenge_request_msg_hash = NULL,
			pp_reconcile_needed = FALSE
		WHERE id = $1 AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight)
	if err != nil {
		return false, xerrors.Errorf("failed to mark dataset as unrecoverable: %w", err)
	}
	if marked == 0 {
		return false, nil
	}

	deletionRequested, err := DeletionRequested(tx, dataSetId)
	if err != nil {
		return false, err
	}
	return !deletionRequested, nil
}

// emitProvingSendErrorAlert records a proving failure for a data set; err must be non-nil.
func emitProvingSendErrorAlert(al curioalerting.AlertingInterface, subsystem string, dataSetId int64, err error) {
	_ = al.EmitEvent(context.Background(), curioalerting.AlertEvent{
		System:    alertType,
		Subsystem: subsystem,
		Message:   err.Error(),
		Context: map[string]string{
			curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetId, 10),
		},
	})
}

// emitProvingStoppedAlert tells the operator that proving stopped for a data set.
func emitProvingStoppedAlert(al curioalerting.AlertingInterface, subsystem string, dataSetId int64) {
	_ = al.EmitEvent(context.Background(), curioalerting.AlertEvent{
		System:    alertType,
		Subsystem: subsystem,
		Message:   fmt.Sprintf("Proving stopped for dataset %d: its service ended or it was removed on chain. Curio will not delete it on its own.", dataSetId),
		Context: map[string]string{
			curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetId, 10),
		},
	})
}
