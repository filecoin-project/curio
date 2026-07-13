package contract

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/api"
)

// EthCallTimeout is the maximum duration for any eth_call RPC operation.
// Any problems with RPC connections (e.g. WebSocket disconnects) should be
// detected and handled within this timeout as a last resort to bound the damage.
const EthCallTimeout = 10 * time.Second

// ConservativeEnqueuedRemovalsLimit is a curio-side soft ceiling on the number of
// scheduled piece removals we allow to be queued on-chain per data set. It sits well
// below the on-chain MAX_ENQUEUED_REMOVALS (2000) to keep comfortable headroom: once a
// data set's live removal queue reaches this many entries, we stop enqueuing new
// removals for it until the next proving period flushes the queue.
const ConservativeEnqueuedRemovalsLimit = 200

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

// FilCleanupDeposit returns the FIL cleanup deposit required when creating a data set.
// deleteDataSet and cleanupPieces are nonpayable; the deposit is refunded to whoever
// finalizes on-chain cleanup via _finalizeCleanup.
func FilCleanupDeposit(ctx context.Context, ethClient api.EthClientInterface) (*big.Int, error) {
	pdpVerifier, err := NewPDPVerifier(ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("instantiating PDPVerifier: %w", err)
	}

	deposit, err := pdpVerifier.FILCLEANUPDEPOSIT(EthCallOpts(ctx))
	if err != nil {
		return nil, xerrors.Errorf("reading FIL_CLEANUP_DEPOSIT: %w", err)
	}

	return deposit, nil
}
