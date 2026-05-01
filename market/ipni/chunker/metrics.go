package chunker

import (
	"context"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const (
	entryRequestDemand      = "demand"
	entryRequestSpeculative = "speculative"

	entryCacheOriginDemand      = "demand"
	entryCacheOriginSpeculative = "speculative"

	entryCacheStateReady    = "ready"
	entryCacheStateInflight = "inflight"

	entryCacheResultHit  = "hit"
	entryCacheResultMiss = "miss"

	entryReconstructionSourcePDP = "pdp"
	entryReconstructionSourceDB  = "db"
	entryReconstructionSourceCar = "car"

	entryReconstructionResultSuccess = "success"
	entryReconstructionResultError   = "error"
)

var (
	entryCacheHitWaitBuckets       = []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	entryReconstructionTimeBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

	requestTag, _ = tag.NewKey("request")
	resultTag, _  = tag.NewKey("result")
	originTag, _  = tag.NewKey("origin")
	stateTag, _   = tag.NewKey("state")
	sourceTag, _  = tag.NewKey("source")

	entryRequests = stats.Int64(
		"curio_ipni_entry_requests_total",
		"Total number of IPNI entry requests handled by the entry chunker.",
		stats.UnitDimensionless,
	)
	entryCacheLookups = stats.Int64(
		"curio_ipni_entry_cache_lookups_total",
		"Total number of IPNI entry cache lookups.",
		stats.UnitDimensionless,
	)
	entryCacheHits = stats.Int64(
		"curio_ipni_entry_cache_hits_total",
		"Total number of IPNI entry cache hits by request type, cache origin, and readiness state.",
		stats.UnitDimensionless,
	)
	entryCacheHitWaitDuration = stats.Float64(
		"curio_ipni_entry_cache_hit_wait_seconds",
		"Duration callers wait after hitting the IPNI entry cache.",
		stats.UnitSeconds,
	)
	entrySpeculativeUnused = stats.Int64(
		"curio_ipni_entry_speculative_unused_total",
		"Total number of speculative IPNI entry cache fills evicted without being consumed by a demand request.",
		stats.UnitDimensionless,
	)
	entryReconstructionDuration = stats.Float64(
		"curio_ipni_entry_reconstruction_seconds",
		"Duration of IPNI entry reconstruction after cache miss.",
		stats.UnitSeconds,
	)
)

func init() {
	err := view.Register(
		&view.View{
			Measure:     entryRequests,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{requestTag},
		},
		&view.View{
			Measure:     entryCacheLookups,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{requestTag, resultTag},
		},
		&view.View{
			Measure:     entryCacheHits,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{requestTag, originTag, stateTag},
		},
		&view.View{
			Measure:     entryCacheHitWaitDuration,
			Aggregation: view.Distribution(entryCacheHitWaitBuckets...),
			TagKeys:     []tag.Key{requestTag, originTag, stateTag},
		},
		&view.View{
			Measure:     entrySpeculativeUnused,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     entryReconstructionDuration,
			Aggregation: view.Distribution(entryReconstructionTimeBuckets...),
			TagKeys:     []tag.Key{requestTag, sourceTag, resultTag},
		},
	)
	if err != nil {
		panic(err)
	}
}

func entryRequestLabel(speculated bool) string {
	if speculated {
		return entryRequestSpeculative
	}
	return entryRequestDemand
}

func entryCacheOriginLabel(speculated bool) string {
	if speculated {
		return entryCacheOriginSpeculative
	}
	return entryCacheOriginDemand
}

func recordEntryRequest(request string) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(requestTag, request),
	}, entryRequests.M(1))
}

func recordEntryCacheLookup(request, result string) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(requestTag, request),
		tag.Upsert(resultTag, result),
	}, entryCacheLookups.M(1))
}

func recordEntryCacheHit(request, origin, state string) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(requestTag, request),
		tag.Upsert(originTag, origin),
		tag.Upsert(stateTag, state),
	}, entryCacheHits.M(1))
}

func observeEntryCacheHitWait(request, origin, state string, took time.Duration) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(requestTag, request),
		tag.Upsert(originTag, origin),
		tag.Upsert(stateTag, state),
	}, entryCacheHitWaitDuration.M(took.Seconds()))
}

func recordEntrySpeculativeUnused() {
	stats.Record(context.Background(), entrySpeculativeUnused.M(1))
}

func observeEntryReconstruction(request, source string, took time.Duration, err error) {
	result := entryReconstructionResultSuccess
	if err != nil {
		result = entryReconstructionResultError
	}
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(requestTag, request),
		tag.Upsert(sourceTag, source),
		tag.Upsert(resultTag, result),
	}, entryReconstructionDuration.M(took.Seconds()))
}
