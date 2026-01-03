package robusthttp

import (
	"math"
	"sync"
	"sync/atomic"

	"golang.org/x/xerrors"
)

type RateCounters[K comparable] struct {
	lk              sync.Mutex
	counters        map[K]*RateCounter
	globalTransfers atomic.Int64

	rateFunc RateFunc
}

var TotalTransferDivFactor int64 = 4

type RateFunc func(transferRateMbps float64, peerTransfers, totalTransfers int64) error

func MinAvgGlobalLogPeerRate(minTxRateMbps, linkMbps float64) RateFunc {
	return func(transferRateMbps float64, peerTransfers, totalTransfers int64) error {
		peerTransferFactor := math.Log2(float64(peerTransfers) + 1)
		minPeerTransferRate := minTxRateMbps * peerTransferFactor

		maxAvgTransferRate := linkMbps / float64(totalTransfers*TotalTransferDivFactor)
		if maxAvgTransferRate < minPeerTransferRate {
			minPeerTransferRate = maxAvgTransferRate
		}

		if transferRateMbps < minPeerTransferRate {
			return xerrors.Errorf("transfer rate %.3fMbps less than minimum %.3fMbps (%d peer tx, %d global tx)", transferRateMbps, minPeerTransferRate, peerTransfers, totalTransfers)
		}

		return nil
	}
}

func NewRateCounters[K comparable](rateFunc RateFunc) *RateCounters[K] {
	return &RateCounters[K]{
		counters: make(map[K]*RateCounter),
		rateFunc: rateFunc,
	}
}

func (rc *RateCounters[K]) Get(key K) *RateCounter {
	rc.lk.Lock()
	defer rc.lk.Unlock()

	c, ok := rc.counters[key]
	if !ok {
		c = &RateCounter{
			rateFunc:        rc.rateFunc,
			globalTransfers: &rc.globalTransfers,

			unlink: func(check func() bool) {
				rc.lk.Lock()
				defer rc.lk.Unlock()

				rc.globalTransfers.Add(-1)

				if check() {
					delete(rc.counters, key)
				}
			},
		}
		rc.counters[key] = c
	}

	rc.globalTransfers.Add(1)
	c.transfers.Add(1)

	return c
}

type RateCounter struct {
	transferred atomic.Int64

	lk sync.Mutex

	// only write with RateCounters.lk (inside unlink check func)
	transfers atomic.Int64

	globalTransfers *atomic.Int64

	rateFunc RateFunc
	unlink   func(func() bool)
}

func (rc *RateCounter) Release() {
	rc.lk.Lock()
	defer rc.lk.Unlock()

	rc.release()
}

func (rc *RateCounter) release() {
	rc.unlink(func() bool {
		rc.transfers.Add(-1)
		return rc.transfers.Load() == 0
	})
}

// Check allows only single concurrent check per peer - this is to prevent
// multiple concurrent checks causing all transfers to fail at once.
// When we drop a peer, we'll reduce rc.transfers, so the next check will
// require less total bandwidth (assuming that MinAvgGlobalLogPeerRate is used).
func (rc *RateCounter) Check(cb func() error) error {
	rc.lk.Lock()
	defer rc.lk.Unlock()

	err := cb()
	if err != nil {
		rc.release()
	}

	return err
}
