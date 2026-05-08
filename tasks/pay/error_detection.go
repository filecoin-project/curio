package pay

import (
	"encoding/hex"
	"strings"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
)

// ErrSelectorRailInactiveOrSettled is the hex-encoded 4-byte selector for
// FilecoinPayV1's RailInactiveOrSettled(uint256). Reverts carrying it prove
// the rail has been finalised and zeroed.
var ErrSelectorRailInactiveOrSettled string

func init() {
	parsed, err := filecoinpayment.PaymentsMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FilecoinPay Payments ABI: " + err.Error())
	}
	e, ok := parsed.Errors["RailInactiveOrSettled"]
	if !ok {
		panic("FilecoinPay Payments ABI missing RailInactiveOrSettled error")
	}
	ErrSelectorRailInactiveOrSettled = hex.EncodeToString(e.ID[:4])
}

// IsRailInactiveOrSettledError reports whether err carries the
// RailInactiveOrSettled revert, proving the rail has been zeroed during
// finalisation. Distinguishes finalisation from transient RPC errors.
func IsRailInactiveOrSettledError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "0x"+ErrSelectorRailInactiveOrSettled)
}
