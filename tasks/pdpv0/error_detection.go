package pdpv0

import (
	"encoding/hex"
	"strings"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Lazy initialization of error selectors to avoid unnecessary computation on startup.
type CalculateOnce struct {
	once sync.Once
	val  string
}

// computeSelector computes the first 4 bytes of the keccak256 hash of the error signature.
func computeSelector(signature string) string {
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(signature))
	return hex.EncodeToString(hash.Sum(nil)[:4])
}

// Known error selectors indicating permanent dataset termination.
// These are the first 4 bytes of keccak256(error signature) from PDPListener
// callbacks. When PDPVerifier calls a listener and it reverts with one of
// these, the dataset should never be retried.
//
// Unrecognized reverts fall through to Tier 2 (backoff), so this list only
// needs selectors where we want immediate termination rather than retry.
// Additional PDPListener implementations can add their selectors here.
var (
	errSelectorDataSetPaymentBeyondEndEpoch    CalculateOnce // ref: https://github.com/FilOzone/filecoin-services/blob/2b247916ddd33e4112dc69fd3ea4fc88a3976f56/service_contracts/src/Errors.sol#L192
	errSelectorDataSetPaymentAlreadyTerminated CalculateOnce // ref: https://github.com/FilOzone/filecoin-services/blob/2b247916ddd33e4112dc69fd3ea4fc88a3976f56/service_contracts/src/Errors.sol#L157
)

func ErrSelectorDataSetPaymentBeyondEndEpoch() string {
	errSelectorDataSetPaymentBeyondEndEpoch.once.Do(func() {
		errSelectorDataSetPaymentBeyondEndEpoch.val = computeSelector("DataSetPaymentBeyondEndEpoch(uint256,uint256,uint256)")
	})
	return errSelectorDataSetPaymentBeyondEndEpoch.val
}

func ErrSelectorDataSetPaymentAlreadyTerminated() string {
	errSelectorDataSetPaymentAlreadyTerminated.once.Do(func() {
		errSelectorDataSetPaymentAlreadyTerminated.val = computeSelector("DataSetPaymentAlreadyTerminated(uint256)")
	})
	return errSelectorDataSetPaymentAlreadyTerminated.val
}

// IsUnrecoverableError returns true if the error contains a known unrecoverable
// error selector. These errors indicate the dataset should be permanently terminated
// and proving should stop immediately.
func IsUnrecoverableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, ErrSelectorDataSetPaymentBeyondEndEpoch()) ||
		strings.Contains(errStr, ErrSelectorDataSetPaymentAlreadyTerminated())
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
