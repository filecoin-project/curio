package sealsupra

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
)

const alpha = 0.1 // EMA smoothing factor

// pipelinePhase ensures that there is only one pipeline in each phase
// could be a simple lock, but this gives us some stats
type pipelinePhase struct {
	phaseLock  sync.Mutex
	phaseNum   int
	active     int64
	waiting    int64
	ema        float64 // Exponential Moving Average in seconds
	lastLockAt time.Time
}

func (p *pipelinePhase) Lock() {
	atomic.AddInt64(&p.waiting, 1)
	_ = stats.RecordWithTags(context.Background(),
		[]tag.Mutator{tag.Upsert(phaseKey, fmt.Sprintf("phase_%d", p.phaseNum))},
		SupraSealMeasures.PhaseWaitingCount.M(atomic.LoadInt64(&p.waiting)))

	p.phaseLock.Lock()

	atomic.AddInt64(&p.waiting, -1)
	atomic.AddInt64(&p.active, 1)
	_ = stats.RecordWithTags(context.Background(),
		[]tag.Mutator{tag.Upsert(phaseKey, fmt.Sprintf("phase_%d", p.phaseNum))},
		SupraSealMeasures.PhaseLockCount.M(atomic.LoadInt64(&p.active)),
		SupraSealMeasures.PhaseWaitingCount.M(atomic.LoadInt64(&p.waiting)))

	p.lastLockAt = time.Now()
}

func (p *pipelinePhase) Unlock() {
	duration := time.Since(p.lastLockAt)
	durationSeconds := duration.Seconds()

	// Update EMA
	if p.ema == 0 {
		p.ema = durationSeconds // Initialize EMA with first value
	} else {
		p.ema = alpha*durationSeconds + (1-alpha)*p.ema
	}

	atomic.AddInt64(&p.active, -1)
	_ = stats.RecordWithTags(context.Background(),
		[]tag.Mutator{tag.Upsert(phaseKey, fmt.Sprintf("phase_%d", p.phaseNum))},
		SupraSealMeasures.PhaseLockCount.M(atomic.LoadInt64(&p.active)),
		SupraSealMeasures.PhaseAvgDuration.M(p.ema))

	p.phaseLock.Unlock()
}
