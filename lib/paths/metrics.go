package paths

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	updateTagKey, _   = tag.NewKey("update")
	cacheIDTagKey, _  = tag.NewKey("cache_id")
	sealedIDTagKey, _ = tag.NewKey("sealed_id")

	pre = "curio_stor_"

	// Buckets for the duration histogram (in seconds)
	durationBuckets = []float64{0.1, 1, 5, 12, 20, 60, 90, 150, 300, 500, 900, 1500, 3000, 6000, 15000, 30000, 60000, 90000, 200_000, 600_000, 1000_000}
)

var (
	// Measures
	GenerateSingleVanillaProofCalls    = stats.Int64(pre+"generate_single_vanilla_proof_calls", "Number of calls to GenerateSingleVanillaProof", stats.UnitDimensionless)
	GenerateSingleVanillaProofErrors   = stats.Int64(pre+"generate_single_vanilla_proof_errors", "Number of errors in GenerateSingleVanillaProof", stats.UnitDimensionless)
	GenerateSingleVanillaProofDuration = stats.Int64(pre+"generate_single_vanilla_proof_duration_seconds", "Duration of GenerateSingleVanillaProof in seconds", stats.UnitMilliseconds)
	FindSectorUncached                 = stats.Int64(pre+"find_sector_uncached", "Number of findSector uncached calls", stats.UnitDimensionless)
	FindSectorCacheHits                = stats.Int64(pre+"find_sector_cache_hits", "Number of findSectorCache hits", stats.UnitDimensionless)
	FindSectorCacheMisses              = stats.Int64(pre+"find_sector_cache_misses", "Number of findSectorCache misses", stats.UnitDimensionless)
)

func init() {
	err := view.Register(
		&view.View{
			Measure:     GenerateSingleVanillaProofCalls,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{updateTagKey, cacheIDTagKey, sealedIDTagKey},
		},
		&view.View{
			Measure:     GenerateSingleVanillaProofErrors,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{updateTagKey, cacheIDTagKey, sealedIDTagKey},
		},
		&view.View{
			Measure:     GenerateSingleVanillaProofDuration,
			Aggregation: view.Distribution(durationBuckets...),
			TagKeys:     []tag.Key{updateTagKey, cacheIDTagKey, sealedIDTagKey},
		},
		&view.View{
			Measure:     FindSectorUncached,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     FindSectorCacheHits,
			Aggregation: view.Sum(),
		},
		&view.View{
			Measure:     FindSectorCacheMisses,
			Aggregation: view.Sum(),
		},
	)
	if err != nil {
		panic(err)
	}
}
