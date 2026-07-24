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

	dataSetNotFound, ok := parsedPDPVerifier.Errors["DataSetNotFound"]
	if !ok {
		panic("PDPVerifier ABI missing DataSetNotFound error")
	}
	ErrPDPVerifierDataSetNotFound = dataSetNotFound

	dataSetNotLive, ok := parsedPDPVerifier.Errors["DataSetNotLive"]
	if !ok {
		panic("PDPVerifier ABI missing DataSetNotLive error")
	}
	ErrPDPVerifierDataSetNotLive = dataSetNotLive

	insufficientChallengeDelay, ok := parsedPDPVerifier.Errors["InsufficientChallengeDelay"]
	if !ok {
		panic("PDPVerifier ABI missing InsufficientChallengeDelay error")
	}
	ErrPDPVerifierInsufficientChallengeDelay = insufficientChallengeDelay

	excessiveChallengeDelay, ok := parsedPDPVerifier.Errors["ExcessiveChallengeDelay"]
	if !ok {
		panic("PDPVerifier ABI missing ExcessiveChallengeDelay error")
	}
	ErrPDPVerifierExcessiveChallengeDelay = excessiveChallengeDelay

	parsedFWSS, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FWSS ABI: " + err.Error())
	}

	beyondEndEpoch, ok := parsedFWSS.Errors["DataSetPaymentBeyondEndEpoch"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentBeyondEndEpoch error")
	}
	ErrFWSSDataSetPaymentBeyondEndEpoch = beyondEndEpoch

	alreadyTerminated, ok := parsedFWSS.Errors["DataSetPaymentAlreadyTerminated"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentAlreadyTerminated error")
	}
	ErrFWSSDataSetPaymentAlreadyTerminated = alreadyTerminated

	onlyPDPVerifierAllowed, ok := parsedFWSS.Errors["OnlyPDPVerifierAllowed"]
	if !ok {
		panic("FWSS ABI missing OnlyPDPVerifierAllowed error")
	}
	ErrFWSSOnlyPDPVerifierAllowed = onlyPDPVerifierAllowed

	proofAlreadySubmitted, ok := parsedFWSS.Errors["ProofAlreadySubmitted"]
	if !ok {
		panic("FWSS ABI missing ProofAlreadySubmitted error")
	}
	ErrFWSSProofAlreadySubmitted = proofAlreadySubmitted

	invalidChallengeCount, ok := parsedFWSS.Errors["InvalidChallengeCount"]
	if !ok {
		panic("FWSS ABI missing InvalidChallengeCount error")
	}
	ErrFWSSInvalidChallengeCount = invalidChallengeCount

	provingNotStarted, ok := parsedFWSS.Errors["ProvingNotStarted"]
	if !ok {
		panic("FWSS ABI missing ProvingNotStarted error")
	}
	ErrFWSSProvingNotStarted = provingNotStarted

	challengeWindowTooEarly, ok := parsedFWSS.Errors["ChallengeWindowTooEarly"]
	if !ok {
		panic("FWSS ABI missing ChallengeWindowTooEarly error")
	}
	ErrFWSSChallengeWindowTooEarly = challengeWindowTooEarly

	provingPeriodPassed, ok := parsedFWSS.Errors["ProvingPeriodPassed"]
	if !ok {
		panic("FWSS ABI missing ProvingPeriodPassed error")
	}
	ErrFWSSProvingPeriodPassed = provingPeriodPassed

	invalidChallengeEpoch, ok := parsedFWSS.Errors["InvalidChallengeEpoch"]
	if !ok {
		panic("FWSS ABI missing InvalidChallengeEpoch error")
	}
	ErrFWSSInvalidChallengeEpoch = invalidChallengeEpoch

	nextProvingPeriodAlreadyCalled, ok := parsedFWSS.Errors["NextProvingPeriodAlreadyCalled"]
	if !ok {
		panic("FWSS ABI missing NextProvingPeriodAlreadyCalled error")
	}
	ErrFWSSNextProvingPeriodAlreadyCalled = nextProvingPeriodAlreadyCalled

	parsedFWSSStateView, err := FWSS.FilecoinWarmStorageServiceStateViewMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FWSS state view ABI: " + err.Error())
	}

	provingPeriodNotInitialized, ok := parsedFWSSStateView.Errors["ProvingPeriodNotInitialized"]
	if !ok {
		panic("FWSS state view ABI missing ProvingPeriodNotInitialized error")
	}
	ErrFWSSProvingPeriodNotInitialized = provingPeriodNotInitialized
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
func IsNextProvingPeriodEmptyDatasetError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(provingRevertNoLeavesForProvingPeriod))
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
