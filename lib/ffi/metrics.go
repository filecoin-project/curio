package ffi

import (
	"context"
	"sync/atomic"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	phaseKey, _ = tag.NewKey("phase")
	pre         = "cuffi_"
)

var (
	encActiveStart  atomic.Int64
	encActiveTreeD  atomic.Int64
	encActiveEncode atomic.Int64
	encActiveTreeR  atomic.Int64
	encActiveTail   atomic.Int64
)

var Measures = struct {
	encActivePhase *stats.Int64Measure
}{
	encActivePhase: stats.Int64(pre+"snap_enc_active", "Number of tasks in each phase", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{Measure: Measures.encActivePhase, Aggregation: view.LastValue(), TagKeys: []tag.Key{phaseKey}},
	)
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(phaseKey, "start")}, Measures.encActivePhase.M(encActiveStart.Load()))
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(phaseKey, "tree_d")}, Measures.encActivePhase.M(encActiveTreeD.Load()))
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(phaseKey, "encode")}, Measures.encActivePhase.M(encActiveEncode.Load()))
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(phaseKey, "tree_r")}, Measures.encActivePhase.M(encActiveTreeR.Load()))
			_ = stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(phaseKey, "tail")}, Measures.encActivePhase.M(encActiveTail.Load()))

			time.Sleep(5 * time.Second)
		}
	}()
}
