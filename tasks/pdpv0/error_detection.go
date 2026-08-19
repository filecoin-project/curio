package pdpv0

import (
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"

	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// Known contract errors indicating permanent dataset termination.
var (
	ErrFWSSDataSetPaymentBeyondEndEpoch    abi.Error
	ErrFWSSDataSetPaymentAlreadyTerminated abi.Error
	ErrPDPVerifierDataSetNotFound          abi.Error
)

// PDP proving/initPP/nextPP custom errors that should be classified before
// falling back to generic contract-revert alert/retry handling.
var (
	ErrFWSSProofAlreadySubmitted          abi.Error
	ErrFWSSProvingNotStarted              abi.Error
	ErrFWSSChallengeWindowTooEarly        abi.Error
	ErrFWSSProvingPeriodPassed            abi.Error
	ErrFWSSInvalidChallengeEpoch          abi.Error
	ErrFWSSNextProvingPeriodAlreadyCalled abi.Error
	ErrFWSSProvingPeriodNotInitialized    abi.Error

	ErrPDPVerifierDataSetNotLive             abi.Error
	ErrPDPVerifierInsufficientChallengeDelay abi.Error

	// Removal-queue errors from FilOzone/pdp#297. These resolve against the
	// hand-maintained ABI fragment in pdp/contract/removals.go until the
	// generated PDPVerifier bindings carry them.
	ErrPDPVerifierPendingPieceDeletions     abi.Error
	ErrPDPVerifierInvalidPieceDeletionBatch abi.Error
	ErrPDPVerifierEmptyRemovalBatch         abi.Error
	ErrPDPVerifierOnlyStorageProvider       abi.Error
	ErrPDPVerifierNoPiecesToProve           abi.Error

	// Unexpected proving invariant errors. Curio should not produce these in
	// normal PDPv0 initPP/nextPP/prove flow; classify them explicitly so they
	// alert and require investigation instead of entering recovery/backoff paths.
	ErrPDPVerifierExcessiveChallengeDelay abi.Error
	ErrFWSSOnlyPDPVerifierAllowed         abi.Error
	ErrFWSSInvalidChallengeCount          abi.Error
)

// PDPVerifier proving-flow revert reason strings. These are Solidity
// require(..., "reason") failures, so they do not have ABI custom-error selectors.
const (
	provingRevertOnlyStorageProviderCanProve = "Only the storage provider can prove possession"
	provingRevertPrematureProof              = "premature proof"
	provingRevertNoChallengeScheduled        = "no challenge scheduled"
	provingRevertLeafIndexOutOfBounds        = "Leaf index out of bounds"
	provingRevertProofDidNotVerify           = "proof did not verify"
	provingRevertNoLeavesForProvingPeriod    = "can only start proving once leaves are added"
)

func init() {
	parsedPDPVerifier, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		panic("failed to parse PDPVerifier ABI: " + err.Error())
	}

	var ok bool
	ErrPDPVerifierDataSetNotFound, ok = parsedPDPVerifier.Errors["DataSetNotFound"]
	if !ok {
		panic("PDPVerifier ABI missing DataSetNotFound error")
	}

	ErrPDPVerifierDataSetNotLive, ok = parsedPDPVerifier.Errors["DataSetNotLive"]
	if !ok {
		panic("PDPVerifier ABI missing DataSetNotLive error")
	}

	ErrPDPVerifierInsufficientChallengeDelay, ok = parsedPDPVerifier.Errors["InsufficientChallengeDelay"]
	if !ok {
		panic("PDPVerifier ABI missing InsufficientChallengeDelay error")
	}

	ErrPDPVerifierExcessiveChallengeDelay, ok = parsedPDPVerifier.Errors["ExcessiveChallengeDelay"]
	if !ok {
		panic("PDPVerifier ABI missing ExcessiveChallengeDelay error")
	}

	removalQueue := contract.RemovalQueueABI()

	ErrPDPVerifierPendingPieceDeletions, ok = removalQueue.Errors["PendingPieceDeletions"]
	if !ok {
		panic("PDPVerifier removal ABI missing PendingPieceDeletions error")
	}

	ErrPDPVerifierInvalidPieceDeletionBatch, ok = removalQueue.Errors["InvalidPieceDeletionBatch"]
	if !ok {
		panic("PDPVerifier removal ABI missing InvalidPieceDeletionBatch error")
	}

	ErrPDPVerifierEmptyRemovalBatch, ok = removalQueue.Errors["EmptyRemovalBatch"]
	if !ok {
		panic("PDPVerifier removal ABI missing EmptyRemovalBatch error")
	}

	ErrPDPVerifierOnlyStorageProvider, ok = removalQueue.Errors["OnlyStorageProvider"]
	if !ok {
		panic("PDPVerifier removal ABI missing OnlyStorageProvider error")
	}

	ErrPDPVerifierNoPiecesToProve, ok = removalQueue.Errors["NoPiecesToProve"]
	if !ok {
		panic("PDPVerifier removal ABI missing NoPiecesToProve error")
	}

	parsedFWSS, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FWSS ABI: " + err.Error())
	}

	ErrFWSSDataSetPaymentBeyondEndEpoch, ok = parsedFWSS.Errors["DataSetPaymentBeyondEndEpoch"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentBeyondEndEpoch error")
	}

	ErrFWSSDataSetPaymentAlreadyTerminated, ok = parsedFWSS.Errors["DataSetPaymentAlreadyTerminated"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentAlreadyTerminated error")
	}

	ErrFWSSOnlyPDPVerifierAllowed, ok = parsedFWSS.Errors["OnlyPDPVerifierAllowed"]
	if !ok {
		panic("FWSS ABI missing OnlyPDPVerifierAllowed error")
	}

	ErrFWSSProofAlreadySubmitted, ok = parsedFWSS.Errors["ProofAlreadySubmitted"]
	if !ok {
		panic("FWSS ABI missing ProofAlreadySubmitted error")
	}

	ErrFWSSInvalidChallengeCount, ok = parsedFWSS.Errors["InvalidChallengeCount"]
	if !ok {
		panic("FWSS ABI missing InvalidChallengeCount error")
	}

	ErrFWSSProvingNotStarted, ok = parsedFWSS.Errors["ProvingNotStarted"]
	if !ok {
		panic("FWSS ABI missing ProvingNotStarted error")
	}

	ErrFWSSChallengeWindowTooEarly, ok = parsedFWSS.Errors["ChallengeWindowTooEarly"]
	if !ok {
		panic("FWSS ABI missing ChallengeWindowTooEarly error")
	}

	ErrFWSSProvingPeriodPassed, ok = parsedFWSS.Errors["ProvingPeriodPassed"]
	if !ok {
		panic("FWSS ABI missing ProvingPeriodPassed error")
	}

	ErrFWSSInvalidChallengeEpoch, ok = parsedFWSS.Errors["InvalidChallengeEpoch"]
	if !ok {
		panic("FWSS ABI missing InvalidChallengeEpoch error")
	}

	ErrFWSSNextProvingPeriodAlreadyCalled, ok = parsedFWSS.Errors["NextProvingPeriodAlreadyCalled"]
	if !ok {
		panic("FWSS ABI missing NextProvingPeriodAlreadyCalled error")
	}

	parsedFWSSStateView, err := FWSS.FilecoinWarmStorageServiceStateViewMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FWSS state view ABI: " + err.Error())
	}

	ErrFWSSProvingPeriodNotInitialized, ok = parsedFWSSStateView.Errors["ProvingPeriodNotInitialized"]
	if !ok {
		panic("FWSS state view ABI missing ProvingPeriodNotInitialized error")
	}
}

func contractErrorSelector(errDef abi.Error) string {
	return hex.EncodeToString(errDef.ID[:4])
}

// IsUnrecoverableError returns true if the error contains a known unrecoverable
// error selector. These errors indicate the dataset should be permanently terminated
// and proving should stop immediately.
func IsUnrecoverableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrFWSSDataSetPaymentBeyondEndEpoch)) ||
		strings.Contains(errStr, contractErrorSelector(ErrFWSSDataSetPaymentAlreadyTerminated)) ||
		strings.Contains(errStr, contractErrorSelector(ErrPDPVerifierDataSetNotLive))
}

// IsRetrySameProvingPeriodError returns true for prove timing reverts where
// the current prove task should retry without changing dataset failure state.
func IsRetrySameProvingPeriodError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrFWSSChallengeWindowTooEarly)) ||
		strings.Contains(errStr, strings.ToLower(provingRevertPrematureProof))
}

// IsInsufficientChallengeDelayError returns true when initPP/nextPP used a
// challenge epoch too close to the current block. Waiting and resending the same
// transaction cannot fix this because the delay only decreases; the task must
// recompute the challenge epoch from fresh chain/listener state.
func IsInsufficientChallengeDelayError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrPDPVerifierInsufficientChallengeDelay))
}

// IsSkipCurrentProvingPeriodError returns true when provePossession no longer
// needs to submit a proof for the current proving period.
func IsSkipCurrentProvingPeriodError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrFWSSProofAlreadySubmitted)) ||
		strings.Contains(errStr, contractErrorSelector(ErrFWSSProvingPeriodPassed)) ||
		strings.Contains(errStr, strings.ToLower(provingRevertNoChallengeScheduled))
}

// IsNextProvingPeriodAlreadyCalledError returns true when initPP/nextPP learns
// that FWSS has already advanced the proving period.
func IsNextProvingPeriodAlreadyCalledError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrFWSSNextProvingPeriodAlreadyCalled))
}

// IsProvingPeriodNotInitializedError returns true when nextPP discovers local
// state has a prove schedule but FWSS has no initialized proving period.
func IsProvingPeriodNotInitializedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrFWSSProvingPeriodNotInitialized))
}

// IsNextProvingPeriodEmptyDatasetError returns true when PDPVerifier refuses to
// start the next proving period because the current proving set has no leaves.
//
// Both encodings are matched because one Curio build spans two contract
// versions: the condition is a string revert before FilOzone/pdp#297 and the
// NoPiecesToProve custom error afterwards. The underlying requirement --
// dataSetLeafCount > 0 -- is the same, so neither form can be dropped until no
// deployment runs the older contract.
func IsNextProvingPeriodEmptyDatasetError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, strings.ToLower(provingRevertNoLeavesForProvingPeriod)) ||
		strings.Contains(errStr, contractErrorSelector(ErrPDPVerifierNoPiecesToProve))
}

// IsPendingPieceDeletionsError returns true when nextProvingPeriod (or initPP)
// refuses to roll over because the data set still has scheduled removals
// queued. This is recoverable: the drain task processes the queue and the
// proving-period task retries.
func IsPendingPieceDeletionsError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrPDPVerifierPendingPieceDeletions))
}

// IsStaleRemovalQueueViewError returns true when processPieceDeletions rejects
// the requested batch because Curio's view of the queue is out of date -- the
// queue shrank, or emptied, between the read and the send. Re-reading the queue
// and retrying is the correct response.
func IsStaleRemovalQueueViewError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrPDPVerifierInvalidPieceDeletionBatch)) ||
		strings.Contains(errStr, contractErrorSelector(ErrPDPVerifierEmptyRemovalBatch))
}

// IsOnlyStorageProviderError returns true when PDPVerifier rejects a removal
// call because the sender is not the data set's storage provider. This needs
// operator attention rather than a retry.
func IsOnlyStorageProviderError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrPDPVerifierOnlyStorageProvider))
}

// IsPDPVerifierDataSetNotLive returns true when PDPVerifier reports that a data
// set is no longer live. In the removal pipeline this means the data set is
// being deleted or cleaned up, so its removal queue no longer matters.
func IsPDPVerifierDataSetNotLive(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrPDPVerifierDataSetNotLive))
}

// IsRefreshProvingStateError returns true when initPP/nextPP selected a
// challenge epoch that FWSS no longer accepts. The scheduler should retry so it
// recomputes the proving-period calldata from fresh chain/listener state.
func IsRefreshProvingStateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrFWSSInvalidChallengeEpoch))
}

// IsUnexpectedProvingInvariantError returns true for contract reverts that are
// impossible in Curio's normal PDPv0 path if Curio and the deployed contracts
// agree on listener wiring, schedule math, and proof challenge count.
//
// These are intentionally kept out of recovery categories:
//   - ExcessiveChallengeDelay: initPP/nextPP should use listener-derived
//     challenge epochs inside PDPVerifier's allowed finality window.
//   - OnlyPDPVerifierAllowed: Curio sends PDPVerifier calls; FWSS callbacks
//     should be invoked by PDPVerifier, not Curio.
//   - InvalidChallengeCount: Curio generates contract.NumChallenges proofs,
//     which should match FWSS CHALLENGES_PER_PROOF.
//   - Leaf index out of bounds: Curio chooses challenges from PDPVerifier's
//     challenge range before prove send; seeing this during prove send indicates
//     verifier state drift, not normal proving-state refresh.
func IsUnexpectedProvingInvariantError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, contractErrorSelector(ErrPDPVerifierExcessiveChallengeDelay)) ||
		strings.Contains(errStr, contractErrorSelector(ErrFWSSOnlyPDPVerifierAllowed)) ||
		strings.Contains(errStr, contractErrorSelector(ErrFWSSInvalidChallengeCount)) ||
		strings.Contains(errStr, strings.ToLower(provingRevertLeafIndexOutOfBounds))
}

// IsFWSSProvingNotStartedError returns true when PDPVerifier had a non-zero
// challenge epoch and called FWSS possessionProven, but FWSS had no active
// proving deadline. This indicates local/PDPVerifier/FWSS proving state
// divergence, not a timing retry or dataset termination. The prove handler
// should complete the current proof attempt and reset the dataset to initPP
// scheduling so Curio re-establishes a proving deadline before proving again.
func IsFWSSProvingNotStartedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrFWSSProvingNotStarted))
}

// IsOperatorAttentionProvingError returns true for prove call authorization
// errors that should be surfaced instead of retried as normal proving flow.
func IsOperatorAttentionProvingError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, strings.ToLower(provingRevertOnlyStorageProviderCanProve))
}

// IsProofGenerationFailureError returns true when the contract rejected the
// generated proof itself.
func IsProofGenerationFailureError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(provingRevertProofDidNotVerify))
}

// IsPDPVerifierDataSetNotFound returns true if PDPVerifier reports that a data
// set no longer exists. In prove preflight this is terminal for local proving;
// in the deletion pipeline it means on-chain cleanup has finalized.
func IsPDPVerifierDataSetNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), contractErrorSelector(ErrPDPVerifierDataSetNotFound))
}

// IsContractRevert returns true if the error indicates a contract revert.
// Contract reverts mean the on-chain state is rejecting the call - retrying
// immediately is pointless. This includes gas estimation failures due to
// reverts, which is how most failures manifest.
func IsContractRevert(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	// Common patterns indicating contract reverts
	return strings.Contains(errStr, "execution reverted") ||
		strings.Contains(errStr, "vm execution error") ||
		strings.Contains(errStr, "revert reason") ||
		strings.Contains(errStr, "retcode=33") || // EVM revert exit code
		strings.Contains(errStr, "(exit=[33]") || // Filecoin EVM revert format
		strings.Contains(errStr, "contract reverted")
}
