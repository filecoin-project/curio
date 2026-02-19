package pdp

import (
	"strings"
)

// Known error selectors indicating permanent dataset termination.
// These are the first 4 bytes of keccak256(error signature) from PDPListener
// callbacks. When PDPVerifier calls a listener and it reverts with one of
// these, the dataset should never be retried.
//
// Unrecognized reverts fall through to Tier 2 (backoff), so this list only
// needs selectors where we want immediate termination rather than retry.
// Additional PDPListener implementations can add their selectors here.
const (
	// DataSetPaymentBeyondEndEpoch(uint256,uint256,uint256)
	// The dataset's payment period has ended.
	ErrSelectorDataSetPaymentBeyondEndEpoch = "d7c45de5"

	// DataSetPaymentAlreadyTerminated(uint256)
	// The dataset was explicitly terminated.
	ErrSelectorDataSetPaymentAlreadyTerminated = "e3f8fa35"
)

// IsUnrecoverableError returns true if the error contains a known unrecoverable
// error selector. These errors indicate the dataset should be permanently terminated
// and proving should stop immediately.
func IsUnrecoverableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, ErrSelectorDataSetPaymentBeyondEndEpoch) ||
		strings.Contains(errStr, ErrSelectorDataSetPaymentAlreadyTerminated)
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
