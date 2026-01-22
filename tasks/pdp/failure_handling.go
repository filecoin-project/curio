package pdp

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

const (
	// MaxConsecutiveFailures is the threshold for giving up on a dataset.
	// Used by ApplyProvingBackoff: after this many consecutive contract reverts
	// without a successful prove, the dataset is marked as terminated.
	// This gives time for external resolution (e.g., client adds funds).
	MaxConsecutiveFailures = 5

	// BaseBackoffBlocks is the initial delay after the first contract revert.
	// Used by CalculateBackoffBlocks: subsequent failures double this value.
	BaseBackoffBlocks = 100

	// MaxBackoffBlocks prevents unbounded exponential growth.
	// Used by CalculateBackoffBlocks to cap the delay. In practice,
	// MaxConsecutiveFailures is reached before this cap applies.
	MaxBackoffBlocks = 28800
)

// CalculateBackoffBlocks computes exponential backoff: base * 2^(failures-1)
func CalculateBackoffBlocks(failures int) int {
	if failures <= 0 {
		return 0
	}
	backoff := BaseBackoffBlocks << (failures - 1)
	if backoff > MaxBackoffBlocks || backoff <= 0 { // check for overflow
		return MaxBackoffBlocks
	}
	return backoff
}

// MarkDatasetProvingUnrecoverable marks a dataset as having an unrecoverable proving failure.
// This is called when an unrecoverable error (like DataSetPaymentBeyondEndEpoch) is detected.
func MarkDatasetProvingUnrecoverable(ctx context.Context, db *harmonydb.DB, dataSetId int64, currentHeight int64) error {
	_, err := db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET unrecoverable_proving_failure_epoch = $2,
			consecutive_prove_failures = consecutive_prove_failures + 1,
			next_prove_attempt_at = NULL,
			init_ready = FALSE,
			prove_at_epoch = NULL,
			challenge_request_msg_hash = NULL
		WHERE id = $1 AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight)
	return err
}

// ApplyProvingBackoff increments the failure count and sets a backoff period.
// If too many failures occur, marks the dataset as unrecoverable.
// Returns true if the dataset was marked as unrecoverable.
func ApplyProvingBackoff(ctx context.Context, db *harmonydb.DB, dataSetId int64, currentHeight int64) (unrecoverable bool, err error) {
	// Get current failure count
	var currentFailures int
	err = db.QueryRow(ctx, `
		SELECT consecutive_prove_failures FROM pdp_data_sets WHERE id = $1
	`, dataSetId).Scan(&currentFailures)
	if err != nil {
		return false, xerrors.Errorf("failed to get failure count: %w", err)
	}

	newFailures := currentFailures + 1

	if newFailures >= MaxConsecutiveFailures {
		// Too many failures, mark as terminated
		_, err = db.Exec(ctx, `
			UPDATE pdp_data_sets
			SET unrecoverable_proving_failure_epoch = $2,
				consecutive_prove_failures = $3,
				next_prove_attempt_at = NULL,
				init_ready = FALSE,
				prove_at_epoch = NULL,
				challenge_request_msg_hash = NULL
			WHERE id = $1 AND unrecoverable_proving_failure_epoch IS NULL
		`, dataSetId, currentHeight, newFailures)
		if err != nil {
			return false, xerrors.Errorf("failed to mark as terminated: %w", err)
		}
		log.Warnw("Dataset marked as terminated due to repeated failures",
			"dataSetId", dataSetId, "failures", newFailures)
		return true, nil
	}

	// Apply exponential backoff
	backoffBlocks := CalculateBackoffBlocks(newFailures)
	nextAttempt := currentHeight + int64(backoffBlocks)

	_, err = db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET consecutive_prove_failures = $2,
			next_prove_attempt_at = $3
		WHERE id = $1
	`, dataSetId, newFailures, nextAttempt)
	if err != nil {
		return false, xerrors.Errorf("failed to apply backoff: %w", err)
	}

	log.Infow("Backoff applied for proving failure",
		"dataSetId", dataSetId, "failures", newFailures,
		"backoffBlocks", backoffBlocks, "nextAttemptAt", nextAttempt)
	return false, nil
}

// ResetProvingFailures resets the failure count after a successful prove.
func ResetProvingFailures(ctx context.Context, db *harmonydb.DB, dataSetId int64) error {
	_, err := db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET consecutive_prove_failures = 0,
			next_prove_attempt_at = NULL
		WHERE id = $1
	`, dataSetId)
	return err
}

// ensureServiceTermination inserts a dataset into the deletion pipeline.
func ensureServiceTermination(ctx context.Context, db *harmonydb.DB, dataSetID int64) error {
	n, err := db.Exec(ctx, `INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
	}
	if n != 1 && n != 0 {
		return xerrors.Errorf("expected to insert 0 or 1 rows, inserted %d", n)
	}
	return nil
}

// HandleProvingSendError processes errors from sender.Send() calls in proving tasks.
// It implements three-tier error handling:
//   - Tier 1: Known termination errors, mark terminated immediately
//   - Tier 2: Other contract reverts, apply backoff, may terminate after repeated failures
//   - Tier 3: Transient errors, return error for harmony retry
//
// Returns (done, err) where done=true means the task should complete (not retry),
// and err!=nil means harmony should retry the task.
func HandleProvingSendError(ctx context.Context, db *harmonydb.DB, dataSetId int64, currentHeight int64, sendErr error) (done bool, err error) {
	// Tier 1: Known unrecoverable errors, mark as unrecoverable immediately
	if IsUnrecoverableError(sendErr) {
		if markErr := MarkDatasetProvingUnrecoverable(ctx, db, dataSetId, currentHeight); markErr != nil {
			log.Errorw("Failed to mark dataset as unrecoverable", "error", markErr, "dataSetId", dataSetId)
		}
		log.Warnw("Dataset unrecoverable, stopping proving attempts",
			"dataSetId", dataSetId, "error", sendErr)
		if termErr := ensureServiceTermination(ctx, db, dataSetId); termErr != nil {
			log.Errorw("Failed to ensure service termination", "error", termErr, "dataSetId", dataSetId)
		}
		return true, nil
	}

	// Tier 2: Other contract reverts, apply backoff, may become unrecoverable after repeated failures
	if IsContractRevert(sendErr) {
		unrecoverable, backoffErr := ApplyProvingBackoff(ctx, db, dataSetId, currentHeight)
		if backoffErr != nil {
			log.Errorw("Failed to apply backoff", "error", backoffErr, "dataSetId", dataSetId)
		}
		if unrecoverable {
			log.Warnw("Dataset unrecoverable after repeated contract reverts",
				"dataSetId", dataSetId, "error", sendErr)
			if termErr := ensureServiceTermination(ctx, db, dataSetId); termErr != nil {
				log.Errorw("Failed to ensure service termination", "error", termErr, "dataSetId", dataSetId)
			}
		}
		return true, nil // Backoff applied; scheduler query prevents immediate re-scheduling
	}

	// Tier 3: Transient errors, let harmony retry
	return false, sendErr
}
