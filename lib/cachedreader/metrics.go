package cachedreader

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	pre = "curio_cachedreader_"

	// Tag keys
	cacheTypeKey, _ = tag.NewKey("cache_type") // "piece_reader" or "piece_error"
	reasonKey, _    = tag.NewKey("reason")     // eviction reason or error type
)

// CachedReaderMeasures groups all cached reader metrics.
var CachedReaderMeasures = struct {
	CacheHits       *stats.Int64Measure
	CacheMisses     *stats.Int64Measure
	CacheEvictions  *stats.Int64Measure
	CacheSize       *stats.Int64Measure
	CacheRefs       *stats.Int64Measure
	ReaderErrors    *stats.Int64Measure
	ReaderSuccesses *stats.Int64Measure
}{
	CacheHits:       stats.Int64(pre+"cache_hits", "Number of cache hits.", stats.UnitDimensionless),
	CacheMisses:     stats.Int64(pre+"cache_misses", "Number of cache misses.", stats.UnitDimensionless),
	CacheEvictions:  stats.Int64(pre+"cache_evictions", "Number of cache evictions.", stats.UnitDimensionless),
	CacheSize:       stats.Int64(pre+"cache_size", "Current number of entries in cache.", stats.UnitDimensionless),
	CacheRefs:       stats.Int64(pre+"cache_refs", "Number of active references to cached readers.", stats.UnitDimensionless),
	ReaderErrors:    stats.Int64(pre+"reader_errors", "Total number of piece reader errors.", stats.UnitDimensionless),
	ReaderSuccesses: stats.Int64(pre+"reader_successes", "Total number of successful piece reader creations.", stats.UnitDimensionless),
}

func init() {
	err := view.Register(
		&view.View{
			Measure:     CachedReaderMeasures.CacheHits,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{cacheTypeKey},
		},
		&view.View{
			Measure:     CachedReaderMeasures.CacheMisses,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{cacheTypeKey},
		},
		&view.View{
			Measure:     CachedReaderMeasures.CacheEvictions,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{cacheTypeKey, reasonKey},
		},
		&view.View{
			Measure:     CachedReaderMeasures.CacheSize,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{cacheTypeKey},
		},
		&view.View{
			Measure:     CachedReaderMeasures.CacheRefs,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     CachedReaderMeasures.ReaderErrors,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{reasonKey},
		},
		&view.View{
			Measure:     CachedReaderMeasures.ReaderSuccesses,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{},
		},
	)
	if err != nil {
		panic(err)
	}
}
