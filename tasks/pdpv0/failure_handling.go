package pdpv0

import (
	"context"
	"math/big"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

const (
	contractErrorSourceInitPP = "initPP"
	contractErrorSourceNextPP = "nextPP"
	contractErrorSourceProve  = "prove"
)

// MarkDatasetProvingUnrecoverable marks a dataset as having an unrecoverable proving failure.
// This is called when an unrecoverable error (like DataSetPaymentBeyondEndEpoch) is detected.
func MarkDatasetProvingUnrecoverable(tx *harmonydb.Tx, dataSetId int64, currentHeight int64) error {
	_, err := tx.Exec(`
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

// resetProvingFailures resets the failure count after a successful prove.
func resetProvingFailures(tx *harmonydb.Tx, dataSetId int64) error {
	_, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET consecutive_prove_failures = 0,
				next_prove_attempt_at = NULL
		WHERE id = $1
	`, dataSetId)
	return err
}

// skipCurrentOnChainProvingPeriod reconciles a dataset when initPP/nextPP learns
// that the contract has already scheduled the next challenge period.
//
// How: read PDPVerifier's authoritative next challenge epoch, clear the local
// challenge_request_msg_hash so the prove watcher cannot submit for this period,
// and store that on-chain epoch in prove_at_epoch. The nextPP watcher already
// schedules the following period after prove_at_epoch+challenge_window, so this
// leaves Curio in the normal path for the next period without inventing local
// message_waits_eth state.
//
// Why dropping this period is better: Curio proves only after its own
// challenge_request_msg_hash has a successful message_waits_eth row. If that
// local confirmation is missing, fabricating a hash/wait row would make Curio
// prove against state it did not confirm. Retrying initPP/nextPP can also loop
// on an already-applied period transition. Skipping one already-scheduled period
// avoids both cases and lets the existing scheduler recover at the next window.
func skipCurrentOnChainProvingPeriod(ctx context.Context, tx *harmonydb.Tx, ethClient ethchain.EthClient, dataSetId int64, currentHeight int64) error {
	pdpVerifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier: %w", err)
	}

	challengeEpoch, err := pdpVerifier.GetNextChallengeEpoch(contract.EthCallOpts(ctx), big.NewInt(dataSetId))
	if err != nil {
		return xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}
	if challengeEpoch == nil {
		return xerrors.Errorf("next challenge epoch is nil for data set %d", dataSetId)
	}
	if challengeEpoch.Sign() == 0 {
		return xerrors.Errorf("data set %d has no scheduled challenge epoch on-chain", dataSetId)
	}
	if !challengeEpoch.IsInt64() {
		return xerrors.Errorf("next challenge epoch %s does not fit in int64 for data set %d", challengeEpoch.String(), dataSetId)
	}

	// prev_challenge_request_epoch normally records the epoch where Curio sent
	// initPP/nextPP. In this recovery path that exact send epoch is not useful,
	// because the local challenge_request_msg_hash is intentionally dropped; use
	// the reconciliation height as a marker while prove_at_epoch drives scheduling.
	affected, err := tx.Exec(`
			UPDATE pdp_data_sets
			SET challenge_request_msg_hash = NULL,
				prev_challenge_request_epoch = $2,
				prove_at_epoch = $3,
				consecutive_prove_failures = 0,
				next_prove_attempt_at = NULL
			WHERE id = $1
			  AND unrecoverable_proving_failure_epoch IS NULL
	`, dataSetId, currentHeight, challengeEpoch.Int64())
	if err != nil {
		return xerrors.Errorf("failed to skip current proving period: %w", err)
	}
	if affected != 1 {
		return xerrors.Errorf("expected to skip current proving period for 1 data set, updated %d", affected)
	}

	return nil
}

// HandleProvingSendError processes errors from sender.Send() calls in proving tasks.
// It handles known proving errors before falling back to generic contract-revert
// alerting:
//   - Known termination errors, mark terminated immediately
//   - Same-period timing errors, let harmony retry the current task
//   - Other contract reverts, alert and retry without mutating proving state
//   - Transient errors, return error for harmony retry
//
// Returns (err) where err==nil means the task should complete (not retry),
// and err!=nil means harmony should retry the task.
func HandleProvingSendError(ctx context.Context, tx *harmonydb.Tx, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, dataSetId int64, currentHeight int64, sendErr error, source string) error {

	raiseAlert := func(al curioalerting.AlertingInterface, err error, source string) {
		var ss string
		switch source {
		case contractErrorSourceProve:
			ss = alertNameProving
		case contractErrorSourceInitPP:
			ss = alertNameInitPP
		case contractErrorSourceNextPP:
			ss = alertNameNextPP
		default:
			ss = "Unknown"
		}
		_ = al.EmitEvent(context.Background(), curioalerting.AlertEvent{
			System:    alertType,
			Subsystem: ss,
			Message:   err.Error(),
		})
	}

	switch {
	case IsRetrySameProvingPeriodError(sendErr):
		// Source: prove. The proof is valid but too early for chain height.
		// Return an error so Harmony retries the same prove task with RetryWait.
		log.Warnw("Retrying same proving period after timing revert",
			"dataSetId", dataSetId, "source", source, "height", currentHeight, "error", sendErr)
		return sendErr
	case IsInsufficientChallengeDelayError(sendErr):
		// Source: initPP/nextPP. The challenge epoch was too close to the
		// current block. Retry the task so it recomputes challenge state and
		// calldata instead of resending the same transaction.
		// This should never happen and if it ever happens due to some extreme corner case,
		// we should raise an alert
		raiseAlert(al, sendErr, source)
		log.Warnw("Retrying proving period scheduling after insufficient challenge delay",
			"dataSetId", dataSetId, "source", source, "height", currentHeight, "error", sendErr)
		return sendErr
	case IsUnrecoverableError(sendErr):
		// Source: initPP/nextPP/prove through FWSS payment state. Proving is
		// permanently unrecoverable; mark local state terminal and terminate FWSS.
		if markErr := MarkDatasetProvingUnrecoverable(tx, dataSetId, currentHeight); markErr != nil {
			log.Warnw("Failed to mark dataset as unrecoverable", "error", markErr, "dataSetId", dataSetId)
			return xerrors.Errorf("failed to mark dataset as unrecoverable: %w", markErr)
		}
		log.Warnw("Dataset unrecoverable, stopping proving attempts",
			"dataSetId", dataSetId, "source", source, "error", sendErr)
		if termErr := FWSS.EnsureServiceTermination(tx, dataSetId); termErr != nil {
			log.Warnw("Failed to ensure service termination", "error", termErr, "dataSetId", dataSetId)
			return xerrors.Errorf("failed to ensure service termination: %w", termErr)
		}
		return nil
	case IsSkipCurrentProvingPeriodError(sendErr):
		// Source: prove, or initPP/nextPP for an already advanced period. This
		// is not a proving failure and must not mutate failure/termination state.
		if source == contractErrorSourceProve {
			log.Warnw("Skipping current proof after known contract revert",
				"dataSetId", dataSetId, "source", source, "height", currentHeight, "error", sendErr)
			err := resetProvingFailures(tx, dataSetId)
			if err != nil {
				return xerrors.Errorf("failed to reset proving failures: %w", err)
			}
			return nil
		}

		// Source: initPP/nextPP. The chain has already advanced the proving
		// period, but Curio cannot safely prove it without its own confirmed
		// challenge request message. Reconcile local schedule state from chain
		// and complete this task so the nextPP watcher can pick up after this
		// proving window closes.
		log.Warnw("Proving period scheduling hit an already-applied period transition",
			"dataSetId", dataSetId, "source", source, "height", currentHeight, "error", sendErr)
		if err := skipCurrentOnChainProvingPeriod(ctx, tx, ethClient, dataSetId, currentHeight); err != nil {
			return xerrors.Errorf("failed to skip current on-chain proving period: %w", err)
		}
		return nil
	case IsUnexpectedProvingInvariantError(sendErr):
		// Source: initPP/nextPP/prove, but not expected in Curio's normal path.
		// These mean a contract/Curio invariant was violated: listener challenge
		// schedule outside PDPVerifier's allowed delay, FWSS called by a non-
		// PDPVerifier address, or Curio proof count not matching FWSS. Alert and
		// retry without mutating proving state so the condition is investigated.
		raiseAlert(al, sendErr, source)
		return sendErr
	case IsRefreshProvingStateError(sendErr):
		// Source: prove. The generated proof was based on stale piece/challenge
		// state. Retry the task so it rebuilds the proof from fresh state.
		//
		// Source: initPP/nextPP. The selected challenge epoch is no longer valid.
		// Retry the task so it recomputes the proving schedule from chain/listener state.
		return sendErr
	case IsOperatorAttentionProvingError(sendErr):
		// Source: prove. PDPVerifier rejected the caller before proof checking.
		// This indicates Curio is proving with the wrong storage-provider
		// identity, not a retryable proving state issue.
		raiseAlert(al, sendErr, source)
		return sendErr
	case IsProofGenerationFailureError(sendErr):
		// Source: prove. PDPVerifier rejected the generated proof. This is not
		// recoverable by resending the same proof and should not be handled as a
		// generic contract revert. Surface it for operator attention.
		raiseAlert(al, sendErr, source)
		return sendErr
	case IsFWSSProvingNotStartedError(sendErr):
		// Source: prove. PDPVerifier accepted the proof path far enough to call
		// FWSS, but FWSS has no active proving deadline. This is contract/listener
		// state divergence, not a proof retry or termination condition.
		raiseAlert(al, sendErr, source)
		return sendErr
	case IsContractRevert(sendErr):
		// Source: initPP/nextPP/prove. Fallback for unclassified contract
		// reverts; alert and let Harmony retry without mutating proving state.
		raiseAlert(al, sendErr, source)
		return sendErr
	default:
		// Source: initPP/nextPP/prove. Non-contract send failures are
		// transport/sender/task errors; retry without mutating proving state.
		return sendErr
	}
}
