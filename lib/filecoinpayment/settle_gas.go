package filecoinpayment

import (
	"math/big"
	"sync"

	"github.com/filecoin-project/lotus/build/buildconstants"
)

// MAX_DEFERRED_SETTLE_GAS is the settleRail gas estimate at which an active
// rail is settled before its lockup horizon. Each queued rate change adds a
// validator call, so a rail left to the horizon could otherwise outgrow what
// fits in one transaction.
var MAX_DEFERRED_SETTLE_GAS = uint64(buildconstants.BlockGasLimit / 5)

type deferredSettleKey struct {
	settledUpTo string
	queueSize   string
	target      string
}

// deferredSettleGas remembers rails whose last settleRail estimate stayed
// under MAX_DEFERRED_SETTLE_GAS. Re-estimate when settledUpTo, the queue
// length, or the settle target changes. The target can advance while the
// queue length stays the same and pull queued changes into the settlement.
var deferredSettleGas = struct {
	sync.Mutex
	m map[int64]deferredSettleKey
}{m: make(map[int64]deferredSettleKey)}

func deferredSettleGasKnown(railID int64, settledUpTo, queueSize, target *big.Int) bool {
	deferredSettleGas.Lock()
	defer deferredSettleGas.Unlock()
	k, ok := deferredSettleGas.m[railID]
	return ok && k == deferredSettleKey{settledUpTo: settledUpTo.String(), queueSize: queueSize.String(), target: target.String()}
}

func recordDeferredSettleGas(railID int64, settledUpTo, queueSize, target *big.Int) {
	deferredSettleGas.Lock()
	defer deferredSettleGas.Unlock()
	deferredSettleGas.m[railID] = deferredSettleKey{settledUpTo: settledUpTo.String(), queueSize: queueSize.String(), target: target.String()}
}

func pruneDeferredSettleGas(seen map[int64]struct{}) {
	deferredSettleGas.Lock()
	defer deferredSettleGas.Unlock()
	for railID := range deferredSettleGas.m {
		if _, ok := seen[railID]; !ok {
			delete(deferredSettleGas.m, railID)
		}
	}
}
