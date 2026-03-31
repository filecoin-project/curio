package pdpv0

import (
	"encoding/hex"
	"strings"

	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// Known error selectors indicating permanent dataset termination.
// These are the first 4 bytes of keccak256(error signature) from the FWSS
// contract ABI. When PDPVerifier calls a listener and it reverts with one of
// these, the dataset should never be retried.
//
// Unrecognized reverts fall through to Tier 2 (backoff), so this list only
// needs selectors where we want immediate termination rather than retry.
// Additional PDPListener implementations can add their selectors here.
var (
	ErrSelectorDataSetPaymentBeyondEndEpoch    string
	ErrSelectorDataSetPaymentAlreadyTerminated string
)

func init() {
	parsed, err := FWSS.FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FWSS ABI: " + err.Error())
	}

	beyondEndEpoch, ok := parsed.Errors["DataSetPaymentBeyondEndEpoch"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentBeyondEndEpoch error")
	}
	ErrSelectorDataSetPaymentBeyondEndEpoch = hex.EncodeToString(beyondEndEpoch.ID[:4])

	alreadyTerminated, ok := parsed.Errors["DataSetPaymentAlreadyTerminated"]
	if !ok {
		panic("FWSS ABI missing DataSetPaymentAlreadyTerminated error")
	}
	ErrSelectorDataSetPaymentAlreadyTerminated = hex.EncodeToString(alreadyTerminated.ID[:4])
}

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
