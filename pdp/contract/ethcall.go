package contract

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

// EthCallTimeout is the maximum duration for any eth_call RPC operation.
// Any problems with RPC connections (e.g. WebSocket disconnects) should be
// detected and handled within this timeout as a last resort to bound the damage.
const EthCallTimeout = 10 * time.Second

// EthCallOpts returns bind.CallOpts with a timeout-bounded context derived from
// ctx.
// All contract calls in PDP code MUST use this instead of nil CallOpts or a raw
// context.Background().
func EthCallOpts(ctx context.Context) *bind.CallOpts {
	// Cancel func intentionally not deferred: the context outlives this function
	// (returned inside CallOpts for the caller's eth_call). The "leak" is bounded
	// by EthCallTimeout -- the timer goroutine lives at most 10 seconds.
	ctx, cancel := context.WithTimeout(ctx, EthCallTimeout)
	_ = cancel
	return &bind.CallOpts{Context: ctx}
}
