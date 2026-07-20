package filecoinpayment

import (
	"encoding/hex"
	"strings"
)

var (
	// ErrSelectorRailInactiveOrSettled is the hex-encoded 4-byte selector for
	// FilecoinPayV1's RailInactiveOrSettled(uint256). Reverts carrying it prove
	// the rail has been finalised and zeroed.
	ErrSelectorRailInactiveOrSettled string

	skippableSettleRailErrorSelectors map[string]struct{}
)

// skippableSettleRailErrorNames is intentionally Pay-only and only for
// settleRail simulation failures. Validator-specific errors belong in the
// validator resolver; this list is for reverts where another pass can safely
// retry or chain state already made this settlement attempt unnecessary.
var skippableSettleRailErrorNames = []string{
	"CannotSettleFutureEpochs",
	"NoProgressInSettlement",
	"RailInactiveOrSettled",
}

func init() {
	parsed, err := PaymentsMetaData.GetAbi()
	if err != nil {
		panic("failed to parse FilecoinPay Payments ABI: " + err.Error())
	}

	skippableSettleRailErrorSelectors = make(map[string]struct{}, len(skippableSettleRailErrorNames))
	for _, name := range skippableSettleRailErrorNames {
		e, ok := parsed.Errors[name]
		if !ok {
			panic("FilecoinPay Payments ABI missing " + name + " error")
		}

		selector := hex.EncodeToString(e.ID[:4])
		skippableSettleRailErrorSelectors[selector] = struct{}{}
		if name == "RailInactiveOrSettled" {
			ErrSelectorRailInactiveOrSettled = selector
		}
	}
}

func isSkippableSettleRailError(err error) bool {
	if err == nil {
		return false
	}

	for selector := range skippableSettleRailErrorSelectors {
		if errorContainsSelector(err, selector) {
			return true
		}
	}

	return false
}

// IsRailInactiveOrSettledError reports whether err carries the
// RailInactiveOrSettled revert, proving the rail has been zeroed during
// finalisation. Distinguishes finalisation from transient RPC errors.
func IsRailInactiveOrSettledError(err error) bool {
	return errorContainsSelector(err, ErrSelectorRailInactiveOrSettled)
}

func errorContainsSelector(err error, selector string) bool {
	if err == nil || selector == "" {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "0x"+selector)
}
