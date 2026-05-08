package storage_market

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

type marketPipelineKeyT struct{}

// MarketPipelineKey is the context key for market deal pipeline references.
var MarketPipelineKey marketPipelineKeyT

// MarketRef identifies a single deal pipeline row for targeted SignalNext dispatch.
type MarketRef struct {
	ID     string // MK12 UUID or MK20 deal ID
	IsMK12 bool
}

// MarketPipelineRef retrieves the deal identifier stashed during Do via SetMeta.
func MarketPipelineRef(ctx context.Context) (MarketRef, bool) {
	return harmonytask.MetaValue[MarketRef](ctx, MarketPipelineKey)
}
