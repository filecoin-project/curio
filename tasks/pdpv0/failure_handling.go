package pdpv0

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// MarkDatasetProvingUnrecoverable marks a dataset as having an unrecoverable proving failure.
// This is called when an unrecoverable error (like DataSetPaymentBeyondEndEpoch) is detected.
func MarkDatasetProvingUnrecoverable(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) error {
	_, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET unrecoverable_proving_failure_epoch = $2,
			init_ready = FALSE,
			prove_at_epoch = NULL,
			challenge_request_msg_hash = NULL
		WHERE id = $1 AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight)
	return err
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

func emitProvingSendErrorAlert(al curioalerting.AlertingInterface, subsystem string, err error) {
	_ = al.EmitEvent(context.Background(), curioalerting.AlertEvent{
		System:    alertType,
		Subsystem: subsystem,
		Message:   err.Error(),
	})
}
