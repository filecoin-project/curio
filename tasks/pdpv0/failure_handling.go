package pdpv0

import (
	"context"
	"strconv"

	"github.com/filecoin-project/go-state-types/builtin"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// provingRecheckDelayEpochs is how long init and next proving-period
// scheduling wait before trying a dataset again after an unrecognized
// contract revert. One Filecoin day, so a contract correction is picked up
// on the next pass.
const provingRecheckDelayEpochs int64 = builtin.EpochsInDay

// MarkDatasetProvingUnrecoverable marks a dataset as having an unrecoverable proving failure.
// This is called when an unrecoverable error (like DataSetPaymentBeyondEndEpoch) is detected.
func MarkDatasetProvingUnrecoverable(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) error {
	_, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET unrecoverable_proving_failure_epoch = $2,
			init_ready = FALSE,
			prove_at_epoch = NULL,
			challenge_request_msg_hash = NULL,
			pp_reconcile_needed = FALSE
		WHERE id = $1 AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight)
	return err
}

// deferDatasetProvingRecheck delays init and next proving-period scheduling
// for one dataset by provingRecheckDelayEpochs. The caller has a transaction
// and a contract revert Curio cannot repair by repeating the task immediately.
// The proving schedule is left in place. A later successful schedule or an
// explicit reset clears proving_retry_epoch.
func deferDatasetProvingRecheck(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) error {
	_, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET proving_retry_epoch = $2
		WHERE id = $1
		  AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight+provingRecheckDelayEpochs)
	if err != nil {
		return xerrors.Errorf("failed to defer dataset proving recheck: %w", err)
	}
	return nil
}

func markDatasetProvingUnrecoverableAndTerminate(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) error {
	if markErr := MarkDatasetProvingUnrecoverable(tx, dataSetId, currentHeight); markErr != nil {
		log.Warnw("Failed to mark dataset as unrecoverable", "error", markErr, "dataSetId", dataSetId)
		return xerrors.Errorf("failed to mark dataset as unrecoverable: %w", markErr)
	}
	if termErr := FWSS.EnsureServiceTermination(tx, dataSetId); termErr != nil {
		log.Warnw("Failed to ensure service termination", "error", termErr, "dataSetId", dataSetId)
		return xerrors.Errorf("failed to ensure service termination: %w", termErr)
	}
	return nil
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
