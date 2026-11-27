package pdpv0

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

// provingRecheckDelayEpochs is how long init and next proving-period
// scheduling wait before trying a dataset again after an unrecognized
// contract revert. One Filecoin day, so a contract correction is picked up
// on the next pass.
const provingRecheckDelayEpochs int64 = builtin.EpochsInDay

type provingAlert int

const (
	provingAlertNone provingAlert = iota
	provingAlertSendError
	provingAlertProvingStopped
)

// markDatasetProvingUnrecoverable stops proving for a data set that can no
// longer be proven (for example after DataSetPaymentBeyondEndEpoch). When this
// call set the mark and no deletion of the data set is in progress, it records
// the data set in pdp_data_set_review and reports that the operator still has
// to be told.
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

	deleting, err := DeletionInProgress(tx, dataSetId)
	if err != nil {
		return false, err
	}
	if deleting {
		return false, nil
	}

	_, err = tx.Exec(`
		INSERT INTO pdp_data_set_review (data_set)
		VALUES ($1)
		ON CONFLICT (data_set) DO NOTHING
	`, dataSetId)
	if err != nil {
		return false, xerrors.Errorf("failed to record dataset for review: %w", err)
	}
	return true, nil
}

// claimDatasetProvingPeriodTask assigns taskID to one dataset. The caller is
// inside the task-creation transaction and has the chain height from the
// selection that queued this claim. The cooldown is checked again here because
// that selection runs outside the transaction: another node may already have
// deferred the dataset and finished, clearing the task reference.
func claimDatasetProvingPeriodTask(tx *harmonydb.Tx, taskID harmonytask.TaskID, dataSetId int64, currentHeight int64) (bool, error) {
	affected, err := tx.Exec(`
		UPDATE pdp_data_sets
		SET challenge_request_task_id = $1
		WHERE id = $2
		  AND challenge_request_task_id IS NULL
		  AND (proving_retry_epoch IS NULL OR proving_retry_epoch <= $3)
	`, taskID, dataSetId, currentHeight)
	if err != nil {
		return false, xerrors.Errorf("failed to claim dataset proving period task: %w", err)
	}
	return affected == 1, nil
}

// provingPeriodRecheckCooling reports whether a dataset must wait before
// another init or next proving-period attempt. retryEpoch is the dataset's
// proving_retry_epoch. A null epoch is not cooling.
func provingPeriodRecheckCooling(retryEpoch sql.NullInt64, currentHeight int64) bool {
	return retryEpoch.Valid && retryEpoch.Int64 > currentHeight
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
