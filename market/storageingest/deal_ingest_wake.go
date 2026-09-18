package storageingest

import (
	"context"
	"sort"
	"time"
)

func newSealWake() chan struct{} {
	wake := make(chan struct{}, 1)
	wakeSealLoop(wake)
	return wake
}

func wakeSealLoop(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

func runSealLoop(ctx context.Context, ticks <-chan time.Time, wake <-chan struct{}, seal func() error, onError func(error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-wake:
		}

		if ctx.Err() != nil {
			return
		}
		if err := seal(); err != nil {
			onError(err)
		}
	}
}

func sortedProviderIDs(details map[int64]*mdetails) []int64 {
	providers := make([]int64, 0, len(details))
	for spID := range details {
		providers = append(providers, spID)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	return providers
}
